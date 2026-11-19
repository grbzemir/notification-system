// Command wsclient canlı durum akışını (WebSocket) terminalde izlemek için küçük bir istemcidir.
//
//	go run ./cmd/wsclient <bildirim-id> [<bildirim-id> ...]
//	go run ./cmd/wsclient all
//
// Adres için WS_URL ortam değişkeni kullanılır (varsayılan ws://localhost:8080/api/v1/ws).
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wsclient <notification-id>... | all")
		os.Exit(2)
	}

	base := os.Getenv("WS_URL")
	if base == "" {
		base = "ws://localhost:8080/api/v1/ws"
	}
	q := url.Values{}
	if os.Args[1] == "all" {
		q.Set("all", "true")
	} else {
		for _, id := range os.Args[1:] {
			q.Add("id", id)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, _, err := websocket.Dial(ctx, base+"?"+q.Encode(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer conn.CloseNow()

	fmt.Fprintln(os.Stderr, "connected; waiting for events (Ctrl+C to quit)")
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		fmt.Println(string(data))
	}
}
