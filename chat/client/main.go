package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"protobuf/gen"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

func closeNow(c *websocket.Conn) {
	err := c.CloseNow()
	if err != nil {
		panic(err.Error())
	}
}

func closeBody(r *http.Response) {
	err := r.Body.Close()
	if err != nil {
		panic(err.Error())
	}
}

func main() {
	var host string
	var room string
	var nick string
	flag.StringVar(&host, "host", "", "Server address")
	flag.StringVar(&room, "room", "", "Chat room")
	flag.StringVar(&nick, "nick", "", "Chat nick")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	url := fmt.Sprintf("%s?nick=%s&room=%s", host, nick, room)
	//noinspection GoResourceLeak
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		panic(err.Error())
	}
	defer closeNow(c)
	var line string
	_, err = fmt.Scanln(&line)
	if err != nil {
		panic(err.Error())
	}
	messageBuilder := messages.MessageContent_builder{}
	messageBuilder.Message = proto.String(line)
	messageBuilder.Severity = messages.MessageSeverity_NORMAL.Enum()
	bytes, err := proto.Marshal(messageBuilder.Build())
	if err != nil {
		panic(err.Error())
	}
	err = c.Write(ctx, websocket.MessageBinary, bytes)
	if err != nil {
		panic(err.Error())
	}
	_, resp, err := c.Read(ctx)
	if err != nil {
		panic(err.Error())
	}
	var serverResponse messages.ServerResponse
	err = proto.Unmarshal(resp, &serverResponse)
	if err != nil {
		panic(err.Error())
	}
	message := serverResponse.GetPayload().GetContent().GetMessage()
	from := serverResponse.GetPayload().GetFrom()
	timestamp := serverResponse.GetTimestamp()
	fmt.Printf("[%s]%s: %s", timestamp.AsTime().Format(time.RFC3339), from, message)
}
