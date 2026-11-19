package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/config"
	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/provider"
	"github.com/grbzemir/notification-system/internal/queue"
	"github.com/grbzemir/notification-system/internal/ratelimit"
	"github.com/grbzemir/notification-system/internal/repository"
	"github.com/grbzemir/notification-system/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.LoadWorker()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Önce havuzu aç: NewPool Postgres hazır olana kadar bağlantıyı yeniden dener.
	pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Worker şemanın hazır olmasında API'ye bağımlı kalmasın. golang-migrate Postgres advisory lock kullanır,
	// API ile aynı anda çalışsalar da migration'lar yarışmaz.
	if err := repository.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	rdb, err := repository.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer rdb.Close()

	repo := repository.NewNotificationRepository(pool)
	client := provider.NewClient(cfg.ProviderURL, cfg.ProviderTimeout)
	limiter := ratelimit.New(ratelimit.NewRedisBackend(rdb, cfg.RateLimitPerSec))
	proc := worker.NewProcessor(repo, client, limiter, cfg.Lease)

	handler := func(ctx context.Context, m queue.Message) error {
		id, err := uuid.Parse(m.ID) // DecodeMessage zaten doğruladı
		if err != nil {
			return nil
		}
		return proc.Process(ctx, id)
	}

	slog.Info("worker starting", "concurrency", cfg.Concurrency, "rate_limit_per_sec", cfg.RateLimitPerSec,
		"provider", cfg.ProviderURL)

	// Her kanal kendi kuyruğunu kendi tüketicisiyle dinler: yavaş bir kanal diğerlerini yavaşlatmaz.
	var wg sync.WaitGroup
	for _, ch := range domain.Channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queue.NewConsumer(cfg.RabbitMQURL, ch, cfg.Concurrency, handler).Run(ctx)
		}()
	}

	<-ctx.Done()
	slog.Info("shutdown signal received, draining in-flight messages")
	wg.Wait()
	slog.Info("worker stopped")
	return nil
}