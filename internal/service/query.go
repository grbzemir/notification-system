package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/events"
)

func (s *NotificationService) Get(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	return s.repo.GetByID(ctx, id)
}

// Cancel yalnızca pending veya scheduled bildirimi iptal eder.
// Bu kuralın atomik uygulanması repository'deki tek UPDATE sorgusundadır.
func (s *NotificationService) Cancel(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	n, err := s.repo.Cancel(ctx, id)
	if err != nil {
		return nil, err
	}
	s.events.Publish(ctx, events.FromNotification(n))
	return n, nil
}

func (s *NotificationService) BatchSummary(ctx context.Context, batchID uuid.UUID) (*domain.BatchSummary, error) {
	return s.repo.BatchSummary(ctx, batchID)
}

// List filtreli ve cursor tabanlı sayfalı liste döner.
// Repository limit+1 kayıt getirir; fazlalık varsa başka sayfa var demektir.
func (s *NotificationService) List(ctx context.Context, f domain.ListFilter) (*domain.Page, error) {
	if f.Limit <= 0 {
		f.Limit = domain.DefaultPageSize
	}
	if f.Limit > domain.MaxPageSize {
		f.Limit = domain.MaxPageSize
	}

	rows, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}

	page := &domain.Page{Items: rows}
	if len(rows) > f.Limit {
		page.Items = rows[:f.Limit]
		last := page.Items[f.Limit-1]
		page.NextCursor = domain.EncodeCursor(domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []*domain.Notification{} // JSON'da null yerine []
	}
	return page, nil
}
