package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grbzemir/notification-system/internal/config"
	"github.com/grbzemir/notification-system/internal/handler"
	"github.com/grbzemir/notification-system/internal/repository"
	"github.com/grbzemir/notification-system/internal/service"
)

const idempotencyTTL = 24 * time.Hour

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := repository.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}
	slog.Info("migrations applied")

	pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("database connected")

	rdb, err := repository.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer rdb.Close()
	slog.Info("redis connected")

	// Bağımlılıkları aşağıdan yukarıya bağlıyoruz: repository -> service -> handler.
	repo := repository.NewNotificationRepository(pool)
	idem := repository.NewIdempotencyStore(rdb, idempotencyTTL)
	svc := service.NewNotificationService(repo, idem)
	nh := handler.NewNotificationHandler(svc)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler.NewRouter(nh),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown: yeni istek almayı bırak, yarım kalanları 10 sn içinde bitir.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
