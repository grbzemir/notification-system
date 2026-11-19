package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// CreateBatch en fazla 1000 bildirimi tek işlemde (atomik) kaydeder ve kuyruğa bırakır.
// Bir öğe bile geçersizse hiçbiri kaydedilmez. Oluşan batch ID'si ve öğe sayısı döner.
func (s *NotificationService) CreateBatch(ctx context.Context, items []domain.NewNotification) (uuid.UUID, int, error) {
	if err := domain.ValidateBatch(items); err != nil {
		return uuid.Nil, 0, err
	}

	batchID := uuid.New()
	notifications := make([]*domain.Notification, len(items))
	for i, in := range items {
		id := batchID // her kayıt kendi kopyasını taşısın
		notifications[i] = &domain.Notification{
			ID:        uuid.New(),
			BatchID:   &id,
			Recipient: in.Recipient,
			Channel:   in.Channel,
			Content:   in.Content,
			Priority:  in.Priority,
			Status:    initialStatus(in),

			ScheduledAt: in.ScheduledAt,
		}
	}

	if err := s.repo.CreateBatch(ctx, notifications); err != nil {
		return uuid.Nil, 0, err
	}

	if ready := publishable(notifications); len(ready) > 0 {
		s.enqueue(ctx, ready...)
	}
	return batchID, len(notifications), nil
}
