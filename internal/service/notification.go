package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// NotificationRepository service'in ihtiyaç duyduğu saklama işlemleridir.
// Interface'i tüketen taraf (service) tanımlar; testte sahte repo vermek kolaylaşır.
type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
}

type NotificationService struct {
	repo NotificationRepository
}

func NewNotificationService(repo NotificationRepository) *NotificationService {
	return &NotificationService{repo: repo}
}

// Create isteği doğrular, bildirimi "pending" olarak kaydeder.
func (s *NotificationService) Create(ctx context.Context, in domain.NewNotification) (*domain.Notification, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	n := &domain.Notification{
		ID:        uuid.New(),
		Recipient: in.Recipient,
		Channel:   in.Channel,
		Content:   in.Content,
		Priority:  in.Priority,
		Status:    domain.StatusPending,
	}
	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}
