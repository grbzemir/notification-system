package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/circuitbreaker"
	"github.com/grbzemir/notification-system/internal/config"
	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/events/redisbus"
	"github.com/grbzemir/notification-system/internal/provider"
	"github.com/grbzemir/notification-system/internal/queue"
	"github.com/grbzemir/notification-system/internal/ratelimit"
	"github.com/grbzemir/notification-system/internal/relay"
	"github.com/grbzemir/notification-system/internal/repository"
	"github.com/grbzemir/notification-system/internal/retry"
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

	// Relay'in yeniden denemesi zamanı gelen bildirimleri kuyruğa bırakabilmesi için yayıncı gerekir.
	pub, err := queue.NewPublisher(ctx, cfg.RabbitMQURL)
	if err != nil {
		return err
	}
	defer pub.Close()

	repo := repository.NewNotificationRepository(pool)
	client := circuitbreaker.NewSender(
		provider.NewClient(cfg.ProviderURL, cfg.ProviderTimeout),
		circuitbreaker.Config{FailureThreshold: cfg.CircuitFailureThreshold, OpenTimeout: cfg.CircuitOpenTimeout},
	)
	limiter := ratelimit.New(ratelimit.NewRedisBackend(rdb, cfg.RateLimitPerSec))
	policy := retry.Policy{MaxAttempts: cfg.RetryMaxAttempts, Base: cfg.RetryBase, Max: cfg.RetryMax}
	proc := worker.NewProcessor(repo, client, limiter, policy, cfg.Lease).WithEvents(redisbus.New(rdb))

	handler := func(ctx context.Context, m queue.Message) error {
		id, err := uuid.Parse(m.ID) // DecodeMessage zaten doğruladı
		if err != nil {
			return nil
		}
		return proc.Process(ctx, id)
	}

	slog.Info("worker starting", "concurrency", cfg.Concurrency, "rate_limit_per_sec", cfg.RateLimitPerSec,
		"provider", cfg.ProviderURL, "retry_max_attempts", cfg.RetryMaxAttempts,
		"retry_base", cfg.RetryBase.String(), "retry_max", cfg.RetryMax.String(),
		"circuit_failure_threshold", cfg.CircuitFailureThreshold, "circuit_open_timeout", cfg.CircuitOpenTimeout.String())

	var wg sync.WaitGroup

	// Zamanı gelen yeniden denemeleri kuyruğa bırakır. API'deki relay ile aynı anda çalışabilir (SKIP LOCKED).
	wg.Add(1)
	go func() {
		defer wg.Done()
		relay.New(repo, pub).Run(ctx)
	}()

	// Her kanal kendi kuyruğunu kendi tüketicisiyle dinler: yavaş bir kanal diğerlerini yavaşlatmaz.
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
