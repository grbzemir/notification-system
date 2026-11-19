package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/grbzemir/notification-system/internal/domain"
)

// NotificationRepository bildirimleri PostgreSQL'de saklar.
type NotificationRepository struct {
	pool *pgxpool.Pool
}

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

// Create bildirimi ekler ve veritabanının atadığı alanları (attempts, zaman damgaları) n'e yazar.
func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	const q = `
		INSERT INTO notifications (id, recipient, channel, content, priority, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING attempts, created_at, updated_at`

	return r.pool.QueryRow(ctx, q,
		n.ID.String(), n.Recipient, string(n.Channel), n.Content, string(n.Priority), string(n.Status),
	).Scan(&n.Attempts, &n.CreatedAt, &n.UpdatedAt)
}
