package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedis bir Redis istemcisi açar ve bağlantıyı doğrular.
func NewRedis(ctx context.Context, addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})

	var lastErr error
	for i := 0; i < 10; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = client.Ping(pingCtx).Err()
		cancel()
		if lastErr == nil {
			return client, nil
		}
		select {
		case <-ctx.Done():
			client.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	client.Close()
	return nil, fmt.Errorf("ping redis: %w", lastErr)
}
