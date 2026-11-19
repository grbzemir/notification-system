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
	"github.com/grbzemir/notification-system/internal/events/redisbus"
	"github.com/grbzemir/notification-system/internal/handler"
	"github.com/grbzemir/notification-system/internal/queue"
	"github.com/grbzemir/notification-system/internal/realtime"
	"github.com/grbzemir/notification-system/internal/relay"
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

	// Önce havuzu aç: NewPool Postgres hazır olana kadar bağlantıyı yeniden dener.
	// Migrate'in kendi yeniden denemesi yok, bu yüzden DB'nin kabul ettiğinden emin olduktan sonra çalıştırıyoruz.
	pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("database connected")

	if err := repository.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}
	slog.Info("migrations applied")

	rdb, err := repository.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer rdb.Close()
	slog.Info("redis connected")

	pub, err := queue.NewPublisher(ctx, cfg.RabbitMQURL)
	if err != nil {
		return err
	}
	defer pub.Close()
	slog.Info("rabbitmq connected")

	// Bağımlılıkları aşağıdan yukarıya bağlıyoruz: repository -> service -> handler.
	repo := repository.NewNotificationRepository(pool)
	idem := repository.NewIdempotencyStore(rdb, idempotencyTTL)
	bus := redisbus.New(rdb)
	svc := service.NewNotificationService(repo, idem, service.WithPublisher(pub), service.WithEvents(bus))
	tplSvc := service.NewTemplateService(repository.NewTemplateRepository(pool))
	nh := handler.NewNotificationHandler(svc).WithTemplates(tplSvc)
	th := handler.NewTemplateHandler(tplSvc)

	// Worker'ın Redis'e yayınladığı durum olaylarını bağlı WebSocket istemcilerine dağıtır.
	hub := realtime.NewHub()
	go bus.Subscribe(ctx, hub.Broadcast)
	wh := handler.NewWSHandler(hub, svc)

	// Kuyruğa bırakılamamış bildirimleri arka planda yeniden yayınlar. ctx iptal olunca durur.
	go relay.New(repo, pub).Run(ctx)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler.NewRouter(nh, th, wh),
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
