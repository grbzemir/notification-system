// Package ratelimit kanal başına gönderim hızını (ör. 100 mesaj/sn) tüm worker örnekleri arasında paylaşarak sınırlar.
package ratelimit

import (
	"context"
	"time"
)

// Backend tek bir "token al" denemesini yapar. Gerçek uygulaması Redis'tedir (redis.go).
// allowed=false ise retryIn kadar sonra yeniden denenmelidir.
type Backend interface {
	Take(ctx context.Context, key string) (allowed bool, retryIn time.Duration, err error)
}

// Limiter bir Backend'in üstüne "izin çıkana kadar bekle" davranışını ekler.
type Limiter struct {
	backend Backend
	prefix  string
}

func New(b Backend) *Limiter { return &Limiter{backend: b, prefix: "rl:"} }

// Wait kanal için bir gönderim hakkı alınana kadar bekler. ctx iptal olursa veya backend hata verirse döner.
func (l *Limiter) Wait(ctx context.Context, channel string) error {
	key := l.prefix + channel
	for {
		allowed, retryIn, err := l.backend.Take(ctx, key)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		if retryIn < time.Millisecond {
			retryIn = time.Millisecond
		}
		timer := time.NewTimer(retryIn)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
