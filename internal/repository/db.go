package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool bir bağlantı havuzu açar. Veritabanı henüz hazır değilse birkaç kez tekrar dener.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	var lastErr error
	for i := 0; i < 10; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = pool.Ping(pingCtx)
		cancel()
		if lastErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("ping database: %w", lastErr)
}
