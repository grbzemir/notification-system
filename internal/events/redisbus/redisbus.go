// Package redisbus olayları Redis pub/sub üzerinden süreçler arası taşır (worker -> API).
//
// Pub/sub "ateşle ve unut"tur: abone o an bağlı değilse olay kaybolur. Bu bilinçli bir tercihtir; kalıcı durum zaten
// PostgreSQL'dedir ve WebSocket yalnızca anlık bildirimdir (abone olunca güncel durum ayrıca gönderilir).
package redisbus

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/grbzemir/notification-system/internal/events"
)

const channel = "notification-events"

const publishTimeout = 2 * time.Second

type Bus struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Bus { return &Bus{rdb: rdb} }

// Publish olayı yayınlar. Hata iş akışını bozmaz, yalnızca loglanır.
func (b *Bus) Publish(ctx context.Context, e events.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		slog.Warn("event marshal failed", "error", err)
		return
	}
	// Kapanış sinyali (ctx iptali) son olayın yayınlanmasını engellemesin.
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	if err := b.rdb.Publish(pubCtx, channel, data).Err(); err != nil {
		slog.Warn("event publish failed", "id", e.ID, "status", e.Status, "error", err)
	}
}

// Subscribe ctx iptal edilene kadar gelen her olay için handle'ı çağırır. go-redis bağlantı koparsa kendiliğinden yeniden bağlanır.
func (b *Bus) Subscribe(ctx context.Context, handle func(events.Event)) {
	ps := b.rdb.Subscribe(ctx, channel)
	defer ps.Close()

	ch := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var e events.Event
			if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
				slog.Warn("bad event payload", "error", err)
				continue
			}
			handle(e)
		}
	}
}
