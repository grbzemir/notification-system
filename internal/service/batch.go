package service

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/telemetry"
)

// CreateBatch en fazla 1000 bildirimi tek işlemde (atomik) kaydeder ve kuyruğa bırakır.
// Bir öğe bile geçersizse hiçbiri kaydedilmez. Oluşan batch ID'si ve öğe sayısı döner.
func (s *NotificationService) CreateBatch(ctx context.Context, items []domain.NewNotification) (uuid.UUID, int, error) {
	if err := domain.ValidateBatch(items); err != nil {
		return uuid.Nil, 0, err
	}

	ctx, span := telemetry.Start(ctx, "NotificationService.CreateBatch")
	defer span.End()
	traceparent := telemetry.Traceparent(ctx)

	batchID := uuid.New()
	span.SetAttributes(attribute.String("batch.id", batchID.String()), attribute.Int("batch.size", len(items)))
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

			Traceparent: traceparent,
		}
	}

	if err := s.repo.CreateBatch(ctx, notifications); err != nil {
		telemetry.RecordError(span, err)
		return uuid.Nil, 0, err
	}

	if ready := publishable(notifications); len(ready) > 0 {
		s.enqueue(ctx, ready...)
	}
	return batchID, len(notifications), nil
}
