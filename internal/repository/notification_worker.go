package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/grbzemir/notification-system/internal/domain"
)

// Claim bildirimi atomik olarak "processing"e alır. Tek bir UPDATE olduğu için iki worker aynı anda denese de
// yalnızca biri satırı "pending" görür. lease'den uzun süredir "processing"te duran kayıt (sahibi çökmüş) de devralınabilir.
func (r *NotificationRepository) Claim(ctx context.Context, id uuid.UUID, lease time.Duration) (*domain.Notification, error) {
	const q = `UPDATE notifications
		SET status = 'processing', attempts = attempts + 1, updated_at = now()
		WHERE id = $1::text::uuid
		  AND (status = 'pending'
		       OR (status = 'processing' AND updated_at < now() - make_interval(secs => $2)))
		RETURNING ` + notificationColumns

	n, err := scanNotification(r.pool.QueryRow(ctx, q, id.String(), lease.Seconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotClaimable
	}
	return n, err
}

// MarkSent sağlayıcının kabul ettiği bildirimi "sent" olarak kaydeder. Yalnızca "processing" kayıt güncellenir.
func (r *NotificationRepository) MarkSent(ctx context.Context, id uuid.UUID, providerMessageID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE notifications
		SET status = 'sent', provider_message_id = $2, sent_at = now(), updated_at = now(), last_error = NULL
		WHERE id = $1::text::uuid AND status = 'processing'`, id.String(), providerMessageID)
	return err
}

// MarkFailed bildirimi kalıcı olarak "failed" yapar ve nedenini saklar.
func (r *NotificationRepository) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE notifications
		SET status = 'failed', last_error = $2, updated_at = now()
		WHERE id = $1::text::uuid AND status = 'processing'`, id.String(), reason)
	return err
}

// Release gönderim yapılmadan sahiplenmeyi geri bırakır (processing -> pending). Claim'in artırdığı deneme sayısını geri alır.
func (r *NotificationRepository) Release(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE notifications
		SET status = 'pending', attempts = GREATEST(attempts - 1, 0), updated_at = now()
		WHERE id = $1::text::uuid AND status = 'processing'`, id.String())
	return err
}

// Retry bildirimi delay sonra yeniden denenmek üzere "pending"e alır. queued_at sıfırlanır; böylece relay,
// next_attempt_at gelince bildirimi kuyruğa yeniden bırakır. Yeniden deneme zamanlaması RabbitMQ'da değil veritabanında tutulur.
func (r *NotificationRepository) Retry(ctx context.Context, id uuid.UUID, reason string, delay time.Duration) error {
	_, err := r.pool.Exec(ctx, `UPDATE notifications
		SET status = 'pending', queued_at = NULL, next_attempt_at = now() + make_interval(secs => $3),
		    last_error = $2, updated_at = now()
		WHERE id = $1::text::uuid AND status = 'processing'`, id.String(), reason, delay.Seconds())
	return err
}
