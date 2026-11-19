package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/grbzemir/notification-system/internal/domain"
)

// ID listesini uuid[] olarak karşılaştırıyoruz: $1 metin dizisi gelir, SQL içinde uuid dizisine çevrilir.
// API yayin sonrasi isareti. next_attempt_at doluysa (worker bu arada Retry ile yeniden denemeye aldiysa) dokunmaz;
// aksi halde gec kalan bu isaret Retry nin NULL yaptigi queued_at i yeniden doldurup relay in satiri gormesini engellerdi.
const markQueuedSQL = `UPDATE notifications SET queued_at = now() WHERE id = ANY($1::text[]::uuid[]) AND next_attempt_at IS NULL`

// Relay isareti: satir FOR UPDATE ile kilitli oldugu icin kosulsuz isaretler.
const relayMarkQueuedSQL = `UPDATE notifications SET queued_at = now() WHERE id = ANY($1::text[]::uuid[])`

func idStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// MarkQueued bildirimlerin RabbitMQ'ya bırakıldığını kaydeder.
func (r *NotificationRepository) MarkQueued(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, markQueuedSQL, idStrings(ids))
	return err
}

// PublishUnqueued kuyruğa bırakılmamış "pending" kayıtları kilitleyip publish ile yayınlar ve aynı transaction
// içinde işaretler. İki tür kayıt bulur:
//   - ilk deneme: next_attempt_at boş, kayıt minAge'den eski (API kendi yayınını bitirmeye fırsat bulsun diye)
//   - yeniden deneme: next_attempt_at gelmiş
//
// FOR UPDATE SKIP LOCKED sayesinde birden fazla örnek (API/worker relay'leri) aynı satırı birlikte almaz.
// Yayınlanan kayıt sayısını döner.
func (r *NotificationRepository) PublishUnqueued(ctx context.Context, minAge time.Duration, limit int,
	publish func(context.Context, []*domain.Notification) error) (int, error) {

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Commit sonrası Rollback zararsızdır

	items, err := selectUnqueued(ctx, tx, minAge, limit)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	if err := publish(ctx, items); err != nil {
		return 0, err // transaction geri alınır, kayıtlar bir sonraki turda yeniden denenir
	}

	ids := make([]uuid.UUID, len(items))
	for i, n := range items {
		ids[i] = n.ID
	}
	if _, err := tx.Exec(ctx, relayMarkQueuedSQL, idStrings(ids)); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(items), nil
}

func selectUnqueued(ctx context.Context, tx pgx.Tx, minAge time.Duration, limit int) ([]*domain.Notification, error) {
	const q = `SELECT ` + notificationColumns + `
		FROM notifications
		WHERE status = 'pending' AND queued_at IS NULL
		  AND CASE WHEN next_attempt_at IS NULL
		           THEN created_at < now() - make_interval(secs => $1)
		           ELSE next_attempt_at <= now()
		      END
		ORDER BY COALESCE(next_attempt_at, created_at)
		LIMIT $2
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, q, minAge.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
