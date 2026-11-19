// Package relay, veritabanına kaydedilip kuyruğa bırakılamamış bildirimleri kuyruğa taşır (outbox kalıbının hafif hali).
package relay

import (
	"context"
	"log/slog"
	"time"

	"github.com/grbzemir/notification-system/internal/domain"
)

// Source "kuyruğa bırakılmamış" bildirimleri bulup yayınlayan ve işaretleyen veri kaynağıdır.
// Gerçek uygulaması tek bir veritabanı transaction'ı içinde çalışır.
type Source interface {
	PublishUnqueued(ctx context.Context, minAge time.Duration, limit int,
		publish func(context.Context, []*domain.Notification) error) (int, error)
}

type Publisher interface {
	Publish(ctx context.Context, ns []*domain.Notification) error
}

type Relay struct {
	src      Source
	pub      Publisher
	minAge   time.Duration
	batch    int
	interval time.Duration
}

// New varsayılanlar: 10 sn'den eski kayıtlar, tek turda en fazla 500 kayıt, 5 sn'de bir kontrol.
// minAge, API'nin kendi yayınını bitirmesine fırsat verir; relay yalnızca "takılı kalmış" kayıtlara bakar.
func New(src Source, pub Publisher) *Relay {
	return &Relay{src: src, pub: pub, minAge: 10 * time.Second, batch: 500, interval: 5 * time.Second}
}

// Run ctx iptal edilene kadar çalışır.
func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		r.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drain bekleyen kayıt kalmayana kadar tur atar.
func (r *Relay) drain(ctx context.Context) {
	for {
		n, err := r.src.PublishUnqueued(ctx, r.minAge, r.batch, r.pub.Publish)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("relay failed", "error", err)
			}
			return
		}
		if n > 0 {
			slog.Info("relay queued pending notifications", "count", n)
		}
		if n < r.batch {
			return
		}
	}
}
