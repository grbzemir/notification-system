package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/grbzemir/notification-system/internal/domain"
)

const dialTimeout = 3 * time.Second

// DeclareTopology exchange, kuyruk ve binding'leri oluşturur. Tekrar çağrılması güvenlidir (idempotent),
// ama var olan bir kuyruğun ayarlarını DEĞİŞTİREMEZ: ayar değişirse kuyruğun silinip yeniden oluşturulması gerekir.
func DeclareTopology(ch *amqp.Channel) error {
	for _, ex := range []string{ExchangeName, DeadLetterExchange} {
		if err := ch.ExchangeDeclare(ex, "direct", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", ex, err)
		}
	}

	if _, err := ch.QueueDeclare(DeadLetterQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue %s: %w", DeadLetterQueue, err)
	}
	if err := ch.QueueBind(DeadLetterQueue, DeadLetterKey, DeadLetterExchange, false, nil); err != nil {
		return fmt.Errorf("bind queue %s: %w", DeadLetterQueue, err)
	}

	args := amqp.Table{
		"x-max-priority":            int32(MaxPriority),
		"x-dead-letter-exchange":    DeadLetterExchange,
		"x-dead-letter-routing-key": DeadLetterKey,
	}
	for _, c := range domain.Channels {
		q := QueueName(c)
		if _, err := ch.QueueDeclare(q, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare queue %s: %w", q, err)
		}
		if err := ch.QueueBind(q, string(c), ExchangeName, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", q, err)
		}
	}
	return nil
}

// Publisher mesajları RabbitMQ'ya publisher confirm ile bırakır. Bağlantı koparsa bir sonraki yayında yeniden bağlanır.
// amqp.Channel eşzamanlı kullanıma uygun olmadığı için yayınlar mutex ile sıraya alınır.
type Publisher struct {
	url  string
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher broker'a bağlanır. Broker henüz hazır değilse (docker compose başlangıcı) birkaç kez dener.
func NewPublisher(ctx context.Context, url string) (*Publisher, error) {
	p := &Publisher{url: url}

	var lastErr error
	for i := 0; i < 10; i++ {
		p.mu.Lock()
		lastErr = p.connect()
		p.mu.Unlock()
		if lastErr == nil {
			return p, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("connect rabbitmq: %w", lastErr)
}

// connect çağıranın mutex'i tuttuğunu varsayar.
func (p *Publisher) connect() error {
	conn, err := amqp.DialConfig(p.url, amqp.Config{Dial: amqp.DefaultDial(dialTimeout)})
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	if err := DeclareTopology(ch); err != nil {
		conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil { // publisher confirm modunu aç
		conn.Close()
		return err
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *Publisher) reset() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn, p.ch = nil, nil
}

func (p *Publisher) ensureConnected() error {
	if p.conn != nil && !p.conn.IsClosed() && !p.ch.IsClosed() {
		return nil
	}
	p.reset()
	return p.connect()
}

// Publish bildirimleri kanallarına göre kuyruğa bırakır ve broker'ın TÜMÜ için onay vermesini bekler.
// Hata dönerse bazı mesajlar yine de kuyruğa girmiş olabilir; çağıran tarafın tekrar denemesi mesajı çoğaltabilir,
// bu yüzden tüketici (worker) idempotent olmalıdır.
func (p *Publisher) Publish(ctx context.Context, ns []*domain.Notification) error {
	if len(ns) == 0 {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnected(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Önce hepsini gönder, sonra onayları bekle: 1000 mesajı tek tek onaylatmaktan çok daha hızlı.
	confirms := make([]*amqp.DeferredConfirmation, 0, len(ns))
	for _, n := range ns {
		body, err := NewMessage(n).Marshal()
		if err != nil {
			return fmt.Errorf("encode message: %w", err)
		}
		dc, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, ExchangeName, string(n.Channel), false, false,
			amqp.Publishing{
				ContentType:  "application/json",
				DeliveryMode: amqp.Persistent, // diske yazılsın, broker yeniden başlasa da kaybolmasın
				Priority:     PriorityValue(n.Priority),
				MessageId:    n.ID.String(),
				Timestamp:    time.Now(),
				Body:         body,
			})
		if err != nil {
			p.reset()
			return fmt.Errorf("publish: %w", err)
		}
		confirms = append(confirms, dc)
	}

	for _, dc := range confirms {
		acked, err := dc.WaitContext(ctx)
		if err != nil {
			p.reset()
			return fmt.Errorf("wait confirm: %w", err)
		}
		if !acked {
			return errors.New("broker rejected a message (nack)")
		}
	}
	return nil
}

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn, p.ch = nil, nil
	return err
}
