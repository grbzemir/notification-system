package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/events"
)

// NotificationRepository service'in ihtiyaç duyduğu saklama işlemleridir.
type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error)
	CreateBatch(ctx context.Context, items []*domain.Notification) error
	Cancel(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Notification, error)
	BatchSummary(ctx context.Context, batchID uuid.UUID) (*domain.BatchSummary, error)
	MarkQueued(ctx context.Context, ids []uuid.UUID) error
}

// IdempotencyCache key -> bildirim ID eşlemesini hızlı okumak için kullanılır.
// Doğruluğun kaynağı değildir; kaybolursa veya hata verirse veritabanı devreye girer.
type IdempotencyCache interface {
	Get(ctx context.Context, key string) (uuid.UUID, bool, error)
	Set(ctx context.Context, key string, id uuid.UUID) error
}

// Publisher kaydedilmiş bildirimleri kuyruğa bırakır.
type Publisher interface {
	Publish(ctx context.Context, ns []*domain.Notification) error
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, []*domain.Notification) error { return nil }

type NotificationService struct {
	repo      NotificationRepository
	cache     IdempotencyCache
	publisher Publisher
	events    events.Publisher
}

// Option NotificationService'i isteğe bağlı bağımlılıklarla yapılandırır (functional options kalıbı).
type Option func(*NotificationService)

// WithPublisher kuyruğa yayın yapan bileşeni bağlar. Verilmezse yayın yapılmaz (testlerde işe yarar).
func WithPublisher(p Publisher) Option {
	return func(s *NotificationService) { s.publisher = p }
}

// WithEvents durum değişikliği olaylarını (WebSocket için) yayınlayan bileşeni bağlar. Verilmezse olay yayınlanmaz.
func WithEvents(p events.Publisher) Option {
	return func(s *NotificationService) { s.events = p }
}

func NewNotificationService(repo NotificationRepository, cache IdempotencyCache, opts ...Option) *NotificationService {
	s := &NotificationService{repo: repo, cache: cache, publisher: noopPublisher{}, events: events.Noop{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create isteği doğrular ve bildirimi "pending" olarak kaydeder, ardından kuyruğa bırakır.
// idempotencyKey doluysa aynı key ile gelen tekrar istek yeni kayıt açmaz, mevcut kaydı döner.
// Dönen bool, kaydın bu çağrıda yeni oluşturulup oluşturulmadığını söyler.
func (s *NotificationService) Create(ctx context.Context, in domain.NewNotification, idempotencyKey string) (*domain.Notification, bool, error) {
	if err := in.Validate(); err != nil {
		return nil, false, err
	}

	if idempotencyKey != "" {
		// Hızlı yol: key daha önce görüldüyse veritabanına insert denemeden cevap ver.
		if existing, err := s.lookupCached(ctx, idempotencyKey); err != nil {
			return nil, false, err
		} else if existing != nil {
			return replay(existing, in)
		}
	}

	n := &domain.Notification{
		ID:        uuid.New(),
		Recipient: in.Recipient,
		Channel:   in.Channel,
		Content:   in.Content,
		Priority:  in.Priority,
		Status:    initialStatus(in),

		ScheduledAt: in.ScheduledAt,
	}
	if idempotencyKey != "" {
		n.IdempotencyKey = &idempotencyKey
	}

	if err := s.repo.Create(ctx, n); err != nil {
		if idempotencyKey != "" && errors.Is(err, domain.ErrDuplicateKey) {
			// Yarış durumu: başka bir istek aynı key'i bizden önce yazdı. Onun kaydını döneriz.
			existing, gerr := s.repo.GetByIdempotencyKey(ctx, idempotencyKey)
			if gerr != nil {
				return nil, false, gerr
			}
			s.remember(ctx, idempotencyKey, existing.ID)
			return replay(existing, in)
		}
		return nil, false, err
	}

	if idempotencyKey != "" {
		s.remember(ctx, idempotencyKey, n.ID)
	}
	if n.Status == domain.StatusPending { // zamanlanmışları relay, vakti gelince yayımlar
		s.enqueue(ctx, n)
	}
	return n, true, nil
}

// enqueue kaydedilmiş bildirimleri kuyruğa bırakır ve "kuyruğa bırakıldı" diye işaretler.
// Başarısız olursa hata DÖNDÜRMEZ: bildirim veritabanında zaten kalıcı olarak "pending" durumdadır
// ve relay, işaretlenmemiş kayıtları bir süre sonra kuyruğa bırakır.
func (s *NotificationService) enqueue(ctx context.Context, ns ...*domain.Notification) {
	if err := s.publisher.Publish(ctx, ns); err != nil {
		slog.Error("publish failed, relay will retry", "count", len(ns), "error", err)
		return
	}

	ids := make([]uuid.UUID, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
	}
	// Mesaj artık kuyrukta. İstemci bağlantıyı kesip ctx iptal olsa bile işaretleme yapılsın ki mesaj çoğaltılmasın.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.MarkQueued(markCtx, ids); err != nil {
		slog.Warn("mark queued failed, relay may publish a duplicate", "error", err)
	}
}

// lookupCached cache'te key varsa ve kayıt hâlâ veritabanında duruyorsa kaydı döner.
// Cache hatası ölümcül değildir: loglanır ve cache miss gibi davranılır.
func (s *NotificationService) lookupCached(ctx context.Context, key string) (*domain.Notification, error) {
	id, found, err := s.cache.Get(ctx, key)
	if err != nil {
		slog.Warn("idempotency cache read failed, falling back to database", "error", err)
		return nil, nil
	}
	if !found {
		return nil, nil
	}
	existing, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil // cache'teki kayıt bayat, veritabanına bakarak devam et
	}
	if err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *NotificationService) remember(ctx context.Context, key string, id uuid.UUID) {
	if err := s.cache.Set(ctx, key, id); err != nil {
		slog.Warn("idempotency cache write failed", "error", err)
	}
}

// replay aynı key'le gelen ikinci isteği yanıtlar. İçerik farklıysa key kötüye kullanılıyordur.
func replay(existing *domain.Notification, in domain.NewNotification) (*domain.Notification, bool, error) {
	if existing.Recipient != in.Recipient ||
		existing.Channel != in.Channel ||
		existing.Content != in.Content ||
		existing.Priority != in.Priority ||
		!sameInstant(existing.ScheduledAt, in.ScheduledAt) {
		return nil, false, domain.ErrIdempotencyConflict
	}
	return existing, false, nil
}

// initialStatus zamanlanmış bildirimi "scheduled", diğerlerini "pending" olarak başlatır.
func initialStatus(in domain.NewNotification) domain.Status {
	if in.ScheduledAt != nil {
		return domain.StatusScheduled
	}
	return domain.StatusPending
}

// publishable içinden hemen kuyruğa bırakılacakları (zamanlanmamış olanları) ayırır.
func publishable(ns []*domain.Notification) []*domain.Notification {
	var out []*domain.Notification
	for _, n := range ns {
		if n.Status == domain.StatusPending {
			out = append(out, n)
		}
	}
	return out
}

// sameInstant iki zaman damgasını veritabanının hassasiyetiyle (mikrosaniye) karşılaştırır; ikisi de nil ise eşittir.
func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Truncate(time.Microsecond).Equal(b.Truncate(time.Microsecond))
}
