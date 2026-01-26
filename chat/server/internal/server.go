package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	port          int
	router        chi.Router
	reader        *kafka.Reader
	writer        *kafka.Writer
	logger        *zap.SugaredLogger
	mutex         sync.Mutex
	nextSubId     int64
	subscriptions map[int64]chan kafka.Message
}

func NewServer(logger *zap.SugaredLogger,
	port int,
	reader *kafka.Reader,
	writer *kafka.Writer) *Server {
	router := chi.NewRouter()
	server := &Server{
		router:        router,
		reader:        reader,
		port:          port,
		logger:        logger,
		writer:        writer,
		mutex:         sync.Mutex{},
		subscriptions: make(map[int64]chan kafka.Message),
		nextSubId:     1,
	}
	router.Get("/health", server.health)
	router.Get("/ws/chat", server.chat)
	return server
}

func (s *Server) subscribe() (int64, <-chan kafka.Message) {
	channel := make(chan kafka.Message, 1024)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	subId := s.nextSubId
	s.subscriptions[subId] = channel
	s.nextSubId++
	return subId, channel
}

func (s *Server) unsubscribe(subId int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.subscriptions, subId)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) closeWebsocketNow(c *websocket.Conn) {
	err := c.CloseNow()
	if err != nil {
		s.logger.Errorf("Failed to close websocket using CloseNow, error: %v", err)
	}
}

var ErrorBadMessageType = errors.New("text message expected")

type KafkaMessage struct {
	From    string `json:"from"`
	Payload string `json:"payload"`
}

type ResponseMessage struct {
	KafkaMessage
	Timestamp time.Time `json:"timestamp"`
}

var expectedCloseStatuses = NewSetWithValues[websocket.StatusCode](
	websocket.StatusNormalClosure,
	websocket.StatusGoingAway,
	websocket.StatusNoStatusRcvd)

func (s *Server) kafkaWriteMessages(ctx context.Context,
	c *websocket.Conn,
	nick, room string) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msgType, msg, err := c.Read(ctx)
			if err != nil {
				status := websocket.CloseStatus(err)
				if expectedCloseStatuses.Contains(status) {
					s.logger.Infof("Connection with room %s closed with expected status: %d", room, status)
					return nil
				}
				s.logger.Errorf("Fail to read a message from a websocket, close status: %d, error: %v", status, err)
				return err
			}
			if msgType != websocket.MessageText {
				return ErrorBadMessageType
			}
			kafkaMessage := &KafkaMessage{
				From:    nick,
				Payload: string(msg),
			}
			encoded, err := json.Marshal(kafkaMessage)
			if err != nil {
				return err
			}
			err = s.writer.WriteMessages(ctx, kafka.Message{
				Key:   []byte(room),
				Value: encoded,
			})
			if err != nil {
				s.logger.Errorf("Failed to write kafka message, error: %v", err)
				return err
			}
		}
	}
}

func (s *Server) kafkaReadMessages(ctx context.Context,
	c *websocket.Conn,
	channel <-chan kafka.Message,
	room string) error {
	for {
		select {
		case msg := <-channel:
			if string(msg.Key) == room {
				var message KafkaMessage
				_ = json.Unmarshal(msg.Value, &message)
				response := ResponseMessage{
					KafkaMessage: message,
					Timestamp:    msg.Time,
				}
				r, _ := json.Marshal(&response)
				err := c.Write(ctx, websocket.MessageText, r)
				if err != nil {
					status := websocket.CloseStatus(err)
					if expectedCloseStatuses.Contains(status) {
						s.logger.Infof("Connection with room %s closed with expected status: %d", room, status)
						return nil
					}
					s.logger.Errorf("Connection with room %s closed with status %d, error: %v", room, status, err)
					return err
				}
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Server) processKafkaMessages(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, err := s.reader.ReadMessage(ctx)
			if err != nil {
				s.logger.Errorf("Failed to read Kafka Message, error: %v", err)
				return err
			}
			s.mutex.Lock()
			for k, v := range s.subscriptions {
				select {
				case v <- msg:
					s.logger.Infof("Message sent to subscription %d", k)
				default:
					s.logger.Warnf("Channel for subscription %d is full", k)
				}
			}
			s.mutex.Unlock()
		}
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nick := query.Get("nick")
	room := query.Get("room")
	if nick == "" || room == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nick and room query params required"))
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Errorf("Unable to accept websocket request, error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer s.closeWebsocketNow(c)
	subId, channel := s.subscribe()
	defer s.unsubscribe(subId)
	group, ctx := errgroup.WithContext(context.Background())
	group.Go(func() error {
		return s.kafkaWriteMessages(ctx, c, nick, room)
	})
	group.Go(func() error {
		return s.kafkaReadMessages(ctx, c, channel, room)
	})
	err = group.Wait()
	if err != nil {
		s.logger.Errorf("Error group failed, error: %v", err)
		if errors.Is(err, ErrorBadMessageType) {
			err = c.Close(websocket.StatusUnsupportedData, err.Error())
			if err != nil {
				s.logger.Errorf("Failed to close websocket properly, error: %v", err)
				return
			}
		}
	}
	err = c.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		s.logger.Errorf("Failed to close websocket properly, error: %v", err)
		return
	}
}

func (s *Server) Serve() error {
	group, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	group.Go(func() error {
		return s.processKafkaMessages(ctx)
	})
	s.logger.Infof("Listening on port: %d", s.port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", s.port), s.router)
	if err != nil {
		cancel()
		return err
	}
	err = group.Wait()
	cancel()
	return err
}
