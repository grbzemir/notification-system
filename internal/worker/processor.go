// Package worker kuyruktan gelen bildirim kimliğini alıp sağlayıcıya gönderen iş mantığını içerir.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/provider"
)

// Store worker'ın veritabanından ihtiyaç duyduğu işlemler.
type Store interface {
	// Claim bildirimi "processing"e alıp sahiplenir. Sahiplenilemezse domain.ErrNotClaimable döner.
	Claim(ctx context.Context, id uuid.UUID, lease time.Duration) (*domain.Notification, error)
	MarkSent(ctx context.Context, id uuid.UUID, providerMessageID string) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string) error
	// Release sahiplenmeyi geri bırakır (processing -> pending), gönderim yapılmadığında kullanılır.
	Release(ctx context.Context, id uuid.UUID) error
}

type Sender interface {
	Send(ctx context.Context, r provider.Request) (*provider.Response, error)
}

type Limiter interface {
	Wait(ctx context.Context, channel string) error
}

type Processor struct {
	store   Store
	sender  Sender
	limiter Limiter
	lease   time.Duration
}

// NewProcessor lease: bir "processing" kaydının, sahibi çöktü sayılıp başkası tarafından devralınabilmesi için
// geçmesi gereken süre. Provider zaman aşımından ve hız sınırı beklemesinden belirgin biçimde uzun olmalıdır.
func NewProcessor(store Store, sender Sender, limiter Limiter, lease time.Duration) *Processor {
	return &Processor{store: store, sender: sender, limiter: limiter, lease: lease}
}

// Process tek bir bildirimi işler.
//   - nil dönerse mesaj kuyruktan silinir (ack): gönderildi, kalıcı olarak başarısız oldu veya yapacak iş yoktu.
//   - hata dönerse mesaj kuyruğa geri konur (requeue): altyapı sorunu var, daha sonra tekrar denenmeli.
func (p *Processor) Process(ctx context.Context, id uuid.UUID) error {
	n, err := p.store.Claim(ctx, id, p.lease)
	if errors.Is(err, domain.ErrNotClaimable) {
		// Mükerrer teslimat, iptal edilmiş ya da zaten gönderilmiş bildirim. Sessizce atla.
		slog.Debug("skip: notification not claimable", "id", id)
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}

	// Hız sınırı: sahiplendikten SONRA bekliyoruz, çünkü iptal edilmiş bildirimler hız bütçesi harcamasın.
	if err := p.limiter.Wait(ctx, string(n.Channel)); err != nil {
		p.release(ctx, n.ID)
		return fmt.Errorf("rate limit: %w", err)
	}

	resp, err := p.sender.Send(ctx, provider.Request{To: n.Recipient, Channel: string(n.Channel), Content: n.Content})
	if err != nil {
		// Adım 9'da: tekrar denenebilir hatalar retry kuyruğuna gidecek. Şimdilik kalıcı başarısızlık sayıyoruz.
		slog.Warn("provider send failed", "id", n.ID, "channel", n.Channel, "attempt", n.Attempts,
			"retryable", provider.IsRetryable(err), "error", err)
		return p.fail(ctx, n.ID, err)
	}

	// Sağlayıcı mesajı KABUL ETTİ. Kapanış sinyali gelmiş olsa bile sonucu kaydetmeliyiz;
	// kaydetmezsek bildirim "processing"te kalır ve lease dolunca ikinci kez gönderilir.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := p.store.MarkSent(saveCtx, n.ID, resp.MessageID); err != nil {
		slog.Error("sent but could not record result; may be re-sent after lease expires", "id", n.ID, "error", err)
		return fmt.Errorf("mark sent: %w", err)
	}
	slog.Info("notification sent", "id", n.ID, "channel", n.Channel, "provider_message_id", resp.MessageID)
	return nil
}

func (p *Processor) fail(ctx context.Context, id uuid.UUID, cause error) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := p.store.MarkFailed(saveCtx, id, cause.Error()); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

func (p *Processor) release(ctx context.Context, id uuid.UUID) {
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := p.store.Release(relCtx, id); err != nil {
		slog.Warn("release failed; lease expiry will recover it", "id", id, "error", err)
	}
}
