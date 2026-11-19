package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// NotificationRepository service'in ihtiyaç duyduğu saklama işlemleridir.
type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error)
}

// IdempotencyCache key -> bildirim ID eşlemesini hızlı okumak için kullanılır.
// Doğruluğun kaynağı değildir; kaybolursa veya hata verirse veritabanı devreye girer.
type IdempotencyCache interface {
	Get(ctx context.Context, key string) (uuid.UUID, bool, error)
	Set(ctx context.Context, key string, id uuid.UUID) error
}

type NotificationService struct {
	repo  NotificationRepository
	cache IdempotencyCache
}

func NewNotificationService(repo NotificationRepository, cache IdempotencyCache) *NotificationService {
	return &NotificationService{repo: repo, cache: cache}
}

// Create isteği doğrular ve bildirimi "pending" olarak kaydeder.
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
		Status:    domain.StatusPending,
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
	return n, true, nil
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
		existing.Priority != in.Priority {
		return nil, false, domain.ErrIdempotencyConflict
	}
	return existing, false, nil
}
