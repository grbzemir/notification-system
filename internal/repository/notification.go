package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/grbzemir/notification-system/internal/domain"
)

const (
	pgUniqueViolation      = "23505"
	idempotencyUniqueIndex = "uq_notifications_idempotency_key"
	notificationColumns    = `id::text, batch_id::text, idempotency_key, recipient, channel, content,
		priority, status, attempts, last_error, provider_message_id, scheduled_at, sent_at, created_at, updated_at, traceparent`
)

// NotificationRepository bildirimleri PostgreSQL'de saklar.
type NotificationRepository struct {
	pool *pgxpool.Pool
}

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

// Create bildirimi ekler. İdempotency key daha önce kullanılmışsa domain.ErrDuplicateKey döner.
func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	const q = `
		INSERT INTO notifications (id, idempotency_key, recipient, channel, content, priority, status, scheduled_at, next_attempt_at, traceparent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)
		RETURNING attempts, created_at, updated_at`

	err := r.pool.QueryRow(ctx, q,
		n.ID.String(), n.IdempotencyKey, n.Recipient, string(n.Channel), n.Content, string(n.Priority), string(n.Status), n.ScheduledAt, n.Traceparent,
	).Scan(&n.Attempts, &n.CreatedAt, &n.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == idempotencyUniqueIndex {
		return domain.ErrDuplicateKey
	}
	return err
}

func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+notificationColumns+` FROM notifications WHERE id = $1`, id.String())
	return scanNotification(row)
}

func (r *NotificationRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+notificationColumns+` FROM notifications WHERE idempotency_key = $1`, key)
	return scanNotification(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanNotification bir satırı domain.Notification'a çevirir.
// UUID kolonlarını ::text olarak okuyup uygulamada parse ediyoruz, sürücü uyumluluğuna bağımlı kalmamak için.
func scanNotification(row rowScanner) (*domain.Notification, error) {
	var (
		n                         domain.Notification
		id                        string
		batchID                   *string
		channel, priority, status string
	)
	err := row.Scan(&id, &batchID, &n.IdempotencyKey, &n.Recipient, &channel, &n.Content,
		&priority, &status, &n.Attempts, &n.LastError, &n.ProviderMessageID,
		&n.ScheduledAt, &n.SentAt, &n.CreatedAt, &n.UpdatedAt, &n.Traceparent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	if n.ID, err = uuid.Parse(id); err != nil {
		return nil, err
	}
	if batchID != nil {
		b, err := uuid.Parse(*batchID)
		if err != nil {
			return nil, err
		}
		n.BatchID = &b
	}
	n.Channel = domain.Channel(channel)
	n.Priority = domain.Priority(priority)
	n.Status = domain.Status(status)
	return &n, nil
}
