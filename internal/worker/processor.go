// Package worker kuyruktan gelen bildirim kimliğini alıp sağlayıcıya gönderen iş mantığını içerir.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/circuitbreaker"
	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/provider"
	"github.com/grbzemir/notification-system/internal/retry"
)

// Store worker'ın veritabanından ihtiyaç duyduğu işlemler.
type Store interface {
	// Claim bildirimi "processing"e alıp sahiplenir. Sahiplenilemezse domain.ErrNotClaimable döner.
	Claim(ctx context.Context, id uuid.UUID, lease time.Duration) (*domain.Notification, error)
	MarkSent(ctx context.Context, id uuid.UUID, providerMessageID string) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string) error
	// Retry bildirimi delay sonra yeniden denenmek üzere "pending"e alır ve kuyruğa bırakılmamış sayar (relay yeniden yayınlar).
	Retry(ctx context.Context, id uuid.UUID, reason string, delay time.Duration) error
	// Defer bildirimi delay sonra yeniden denenmek üzere "pending"e alır ve Claim'in artırdığı deneme sayısını geri alır.
	// Gönderim hiç denenmediğinde (devre kesici açık) kullanılır: sağlayıcı kesintisi deneme hakkı harcamamalı.
	Defer(ctx context.Context, id uuid.UUID, delay time.Duration) error
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
	policy  retry.Policy
	lease   time.Duration
}

// NewProcessor lease: bir "processing" kaydının, sahibi çöktü sayılıp başkası tarafından devralınabilmesi için geçmesi gereken süre.
// Provider zaman aşımından (5 sn) ve rate limit beklemesinden belirgin biçimde uzun olmalıdır.
func NewProcessor(store Store, sender Sender, limiter Limiter, policy retry.Policy, lease time.Duration) *Processor {
	return &Processor{store: store, sender: sender, limiter: limiter, policy: policy, lease: lease}
}

// Process tek bir bildirimi işler.
//   - nil dönerse mesaj kuyruktan silinir (ack): gönderildi, kalıcı olarak başarısız oldu, yeniden denemeye zamanlandı veya yapacak iş yoktu.
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
		return p.handleSendError(ctx, n, err)
	}

	// Sağlayıcı mesajı KABUL ETTİ. İstemci bağlantıyı koparsa/kapanış sinyali gelirse bile sonucu kaydetmeliyiz;
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

// handleSendError sağlayıcı hatasına göre karar verir:
//   - kalıcı hata (4xx)               -> failed, tekrar denenmez
//   - geçici hata, deneme hakkı bitti -> failed ("vazgeçildi")
//   - geçici hata, hak var            -> backoff + jitter sonrasına zamanlanır (pending + next_attempt_at)
//
// Üçünde de nil dönülür (mesaj ack edilir): sonuç veritabanına yazıldı, yeniden deneme artık kuyruğun değil veritabanının işi.
func (p *Processor) handleSendError(ctx context.Context, n *domain.Notification, err error) error {
	if errors.Is(err, circuitbreaker.ErrOpen) {
		return p.deferUntilCircuitCloses(ctx, n, err)
	}
	retryable := provider.IsRetryable(err)
	slog.Warn("provider send failed", "id", n.ID, "channel", n.Channel, "attempt", n.Attempts,
		"retryable", retryable, "error", err)

	if !retryable {
		return p.fail(ctx, n.ID, err.Error())
	}
	if p.policy.Exhausted(n.Attempts) {
		return p.fail(ctx, n.ID, fmt.Sprintf("gave up after %d attempts: %v", n.Attempts, err))
	}

	var minDelay time.Duration
	var pe *provider.Error
	if errors.As(err, &pe) {
		minDelay = pe.RetryAfter // sağlayıcı "şu kadar sonra dene" dediyse ondan önce denemeyiz
	}
	delay := p.policy.Delay(n.Attempts, minDelay)

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := p.store.Retry(saveCtx, n.ID, err.Error(), delay); err != nil {
		return fmt.Errorf("schedule retry: %w", err)
	}
	slog.Info("retry scheduled", "id", n.ID, "attempt", n.Attempts, "max_attempts", p.policy.MaxAttempts,
		"delay", delay.Round(time.Millisecond).String())
	return nil
}

// minCircuitDefer devre kesici kaynaklı ertelemenin alt sınırı: relay'in tarama aralığından (2 sn) kısa beklemenin anlamı yok.
const minCircuitDefer = 2 * time.Second

// deferUntilCircuitCloses devre açıkken sağlayıcıya hiç gidilmedi: bildirimi deneme hakkı harcamadan, devrenin
// yarı açılacağı zamana erteler. Mesaj ack edilir (nil); yeniden yayını veritabanındaki relay yapar.
func (p *Processor) deferUntilCircuitCloses(ctx context.Context, n *domain.Notification, err error) error {
	delay := minCircuitDefer
	var oe *circuitbreaker.OpenError
	if errors.As(err, &oe) && oe.RetryAfter > delay {
		delay = oe.RetryAfter
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if derr := p.store.Defer(saveCtx, n.ID, delay); derr != nil {
		return fmt.Errorf("defer: %w", derr)
	}
	slog.Info("circuit open, notification deferred", "id", n.ID, "channel", n.Channel, "delay", delay.Round(time.Millisecond).String())
	return nil
}

func (p *Processor) fail(ctx context.Context, id uuid.UUID, reason string) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := p.store.MarkFailed(saveCtx, id, reason); err != nil {
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
