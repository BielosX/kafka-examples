package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/segmentio/kafka-go"
)

type Set[T comparable] struct {
	values map[T]struct{}
}

func NewSet[T comparable]() Set[T] {
	return Set[T]{
		values: make(map[T]struct{}),
	}
}

func (s *Set[T]) Put(value T) {
	s.values[value] = struct{}{}
}

func (s *Set[T]) ToSlice() []T {
	result := make([]T, len(s.values))
	idx := 0
	for k, _ := range s.values {
		result[idx] = k
		idx++
	}
	return result
}

func errorLine(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}

func errorFmt(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, args)
	os.Exit(2)
}

func watch(args []string) {
	var topic string
	var host string
	var offset int64
	var group string
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	fs.StringVar(&topic, "topic", "", "Kafka topic")
	fs.StringVar(&host, "host", "", "Kafka host")
	fs.Int64Var(&offset, "offset", 0, "Offset")
	fs.StringVar(&group, "group", "", "Consumer group")
	_ = fs.Parse(args)
	if topic == "" || host == "" {
		errorLine("Topic and Host required")
	}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{host},
		Topic:   topic,
		GroupID: group,
	})
	defer func(reader *kafka.Reader) {
		_ = reader.Close()
	}(reader)
	if group != "" && offset > 0 {
		errorFmt("Offset should not be used when group is set")
	}
	if group == "" {
		err := reader.SetOffset(offset)
		if err != nil {
			errorFmt("Failed to set offset, error: %v", err)
		}
		fmt.Printf("Watching topic %s\n", topic)
	} else {
		fmt.Printf("Watching topic %s as member of group %s\n", topic, group)
	}
	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			errorFmt("Unable to read message, error: %v", err)
		}
		if len(msg.Key) > 0 {
			fmt.Printf("Key: %s, Value: %s\n", msg.Key, msg.Value)
		} else {
			fmt.Printf("Value: %s\n", msg.Value)
		}
	}
}

func send(args []string) {
	var topic string
	var message string
	var key string
	var host string
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	fs.StringVar(&host, "host", "", "Kafka host")
	fs.StringVar(&topic, "topic", "", "Kafka topic")
	fs.StringVar(&message, "message", "", "Message to send")
	fs.StringVar(&key, "key", "", "Message key")
	_ = fs.Parse(args)
	if topic == "" || host == "" {
		errorLine("Topic and Host required")
	}
	writer := kafka.Writer{
		Addr:                   kafka.TCP(host),
		Topic:                  topic,
		AllowAutoTopicCreation: true,
	}
	err := writer.WriteMessages(context.Background(), kafka.Message{
		Key:   []byte(key),
		Value: []byte(message),
	})
	if err != nil {
		errorFmt("Unable to write message, error: %v", err)
	}
}

func listTopics(args []string) {
	var host string
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	fs.StringVar(&host, "host", "", "Kafka host")
	_ = fs.Parse(args)
	if host == "" {
		errorFmt("Host required")
	}
	conn, err := kafka.Dial("tcp", host)
	if err != nil {
		errorFmt("Unable to connect to %s, error: %v", host, err)
	}
	defer func(conn *kafka.Conn) {
		_ = conn.Close()
	}(conn)
	partitions, err := conn.ReadPartitions()
	if err != nil {
		errorFmt("Unable to list partitions, error: %v", err)
	}
	topics := NewSet[string]()
	for _, p := range partitions {
		topics.Put(p.Topic)
	}
	for _, v := range topics.ToSlice() {
		fmt.Println(v)
	}
}

func createTopic(args []string) {
	var host string
	var topic string
	var partitions int64
	var replication int64
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	fs.StringVar(&host, "host", "", "Kafka host")
	fs.StringVar(&topic, "topic", "", "Kafka Topic")
	fs.Int64Var(&partitions, "partitions", 1, "Number of partitions for topic")
	fs.Int64Var(&replication, "replication", 1, "Topic replication factor")
	_ = fs.Parse(args)
	if host == "" || topic == "" {
		errorLine("Topic and Host required")
	}
	client := kafka.Client{
		Addr: kafka.TCP(host),
	}
	_, err := client.CreateTopics(context.Background(), &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             topic,
			NumPartitions:     int(partitions),
			ReplicationFactor: int(replication),
		}},
	})
	if err != nil {
		errorFmt("Unable to create topic, error: %v", err)
	}
}

const (
	commandWatch       = "watch"
	commandSend        = "send"
	commandListTopics  = "list-topics"
	commandCreateTopic = "create-topic"
)

func main() {
	if len(os.Args) < 2 {
		errorLine("Usage: cli [command] [flags]")
	}
	subArgs := os.Args[2:]
	command := os.Args[1]
	switch command {
	case commandWatch:
		watch(subArgs)
	case commandSend:
		send(subArgs)
	case commandListTopics:
		listTopics(subArgs)
	case commandCreateTopic:
		createTopic(subArgs)
	default:
		errorFmt("no such command: %s", command)
	}
}
