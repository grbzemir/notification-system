// Package events bildirim durum değişikliklerinin (sent, failed, cancelled...) olay biçimini tanımlar.
// Worker olayı yayınlar, API WebSocket istemcilerine iletir; ikisi arasındaki taşıyıcı Redis pub/sub'dır (redisbus).
package events

import (
	"context"
	"time"

	"github.com/grbzemir/notification-system/internal/domain"
)

// Event bir bildirimin yeni durumunu anlatır.
type Event struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	Error         string     `json:"error,omitempty"`           // son hatanın nedeni (retry/failed)
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"` // yeniden deneme zamanlandıysa ne zaman
	At            time.Time  `json:"at"`
}

// Publisher olay yayınlar. En iyi çaba esaslıdır: yayın başarısız olursa iş akışı etkilenmez (hata yutulur/loglanır),
// çünkü gerçek durum her zaman veritabanındadır; WebSocket yalnızca anlık bir bildirimdir.
type Publisher interface {
	Publish(ctx context.Context, e Event)
}

// Noop hiçbir şey yapmayan Publisher'dır.
type Noop struct{}

func (Noop) Publish(context.Context, Event) {}

// FromNotification bildirimin mevcut halinden olay üretir (iptal ve "abone olunca anlık durum" için).
func FromNotification(n *domain.Notification) Event {
	e := Event{ID: n.ID.String(), Status: string(n.Status), Attempts: n.Attempts, At: time.Now().UTC()}
	if n.LastError != nil {
		e.Error = *n.LastError
	}
	return e
}
