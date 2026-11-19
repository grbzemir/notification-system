package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/telemetry"
)

const (
	reconnectDelay = 3 * time.Second
	handlerTimeout = 30 * time.Second
)

// Handler tek bir mesajı işler. nil dönerse mesaj onaylanır (ack), hata dönerse kuyruğa geri konur.
type Handler func(ctx context.Context, msg Message) error

// Consumer bir kanalın kuyruğunu dinler. Aynı anda en fazla `concurrency` mesaj işler.
type Consumer struct {
	url          string
	channel      domain.Channel
	concurrency  int
	handler      Handler
	requeueDelay time.Duration
}

func NewConsumer(url string, ch domain.Channel, concurrency int, h Handler) *Consumer {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Consumer{url: url, channel: ch, concurrency: concurrency, handler: h, requeueDelay: time.Second}
}

// Run ctx iptal edilene kadar çalışır; bağlantı koparsa yeniden bağlanır.
func (c *Consumer) Run(ctx context.Context) {
	for {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Error("consumer stopped, reconnecting", "channel", c.channel, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.DialConfig(c.url, amqp.Config{Dial: amqp.DefaultDial(dialTimeout)})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if err := DeclareTopology(ch); err != nil {
		return err
	}
	// Prefetch = eşzamanlılık: worker kuyruktan en fazla bu kadar işlenmemiş mesaj çeker.
	// Büyük olursa düşük öncelikli mesajlar worker'da birikir ve öncelik sırası bozulur.
	if err := ch.Qos(c.concurrency, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}

	tag := fmt.Sprintf("worker-%s-%d", c.channel, os.Getpid())
	deliveries, err := ch.Consume(QueueName(c.channel), tag, false, false, false, false, nil) // autoAck=false
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	slog.Info("consumer started", "queue", QueueName(c.channel), "concurrency", c.concurrency)

	work := make(chan amqp.Delivery)
	var wg sync.WaitGroup
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range work {
				c.handle(ctx, d)
			}
		}()
	}

	var loopErr error
loop:
	for {
		select {
		case <-ctx.Done():
			_ = ch.Cancel(tag, false) // yeni mesaj almayı bırak, işlenmekte olanları bitir
			break loop
		case d, ok := <-deliveries:
			if !ok {
				loopErr = errors.New("deliveries channel closed")
				break loop
			}
			work <- d // prefetch = concurrency olduğundan boşta bir worker her zaman vardır
		}
	}
	close(work)
	wg.Wait() // işlenmekte olan mesajlar bitsin (graceful shutdown)
	return loopErr
}

func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	msg, err := DecodeMessage(d.Body)
	if err != nil {
		// Zehirli mesaj: tekrar denemek işe yaramaz. requeue=false -> dead letter kuyruğuna gider.
		slog.Error("undecodable message, sending to dead letter", "error", err)
		_ = d.Nack(false, false)
		return
	}

	// İşlem kapanış sinyaliyle yarıda kesilmesin: iptal edilebilir bağlamı koparıp kendi zaman aşımını veriyoruz.
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handlerTimeout)
	defer cancel()

	// API'de başlayan iz mesaj başlıklarından devralınır; bu span, worker tarafındaki işin kökü olur.
	hctx = extractContext(hctx, d.Headers)
	hctx, span := telemetry.Start(hctx, "process "+string(msg.Channel),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", QueueName(c.channel)),
			attribute.String("notification.id", msg.ID),
		))
	defer span.End()

	if err := c.handler(hctx, msg); err != nil {
		telemetry.RecordError(span, err)
		slog.Error("handler failed, requeueing", "id", msg.ID, "channel", msg.Channel, "error", err)
		// Kısa bekleme: altyapı sorunu sürerken aynı mesajı saniyede binlerce kez işleme döngüsünü önler.
		select {
		case <-time.After(c.requeueDelay):
		case <-ctx.Done():
		}
		if err := d.Nack(false, true); err != nil {
			slog.Warn("nack failed", "error", err)
		}
		return
	}
	if err := d.Ack(false); err != nil {
		slog.Warn("ack failed", "id", msg.ID, "error", err)
	}
}
