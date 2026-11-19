package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/grbzemir/notification-system/internal/domain"
)

// CreateBatch tüm bildirimleri tek transaction'da ekler: ya hepsi yazılır ya hiçbiri.
// pgx.Batch sorguları tek ağ turunda gönderir.
func (r *NotificationRepository) CreateBatch(ctx context.Context, items []*domain.Notification) error {
	const q = `
		INSERT INTO notifications (id, batch_id, recipient, channel, content, priority, status, scheduled_at, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Commit sonrası Rollback zararsızdır

	batch := &pgx.Batch{}
	for _, n := range items {
		var batchID *string
		if n.BatchID != nil {
			s := n.BatchID.String()
			batchID = &s
		}
		batch.Queue(q, n.ID.String(), batchID, n.Recipient, string(n.Channel), n.Content, string(n.Priority), string(n.Status), n.ScheduledAt)
	}

	br := tx.SendBatch(ctx, batch)
	for range items {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Cancel bildirimi yalnızca pending/scheduled ise iptal eder. Kontrol ve güncelleme tek sorguda
// olduğu için worker aynı anda göndermeye başlasa bile yarış oluşmaz.
func (r *NotificationRepository) Cancel(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	const q = `
		UPDATE notifications
		SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'scheduled')
		RETURNING ` + notificationColumns

	n, err := scanNotification(r.pool.QueryRow(ctx, q, id.String()))
	if err == nil {
		return n, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	// Satır güncellenmedi: kayıt yok mu, yoksa artık iptal edilemez mi?
	existing, gerr := r.GetByID(ctx, id)
	if gerr != nil {
		return nil, gerr // ErrNotFound dahil
	}
	if existing.Status == domain.StatusCancelled {
		return existing, nil // zaten iptal edilmiş: iptal işlemi idempotent
	}
	return nil, domain.ErrNotCancellable
}

// BatchSummary bir batch'teki bildirimleri duruma göre sayar. Batch yoksa ErrNotFound döner.
func (r *NotificationRepository) BatchSummary(ctx context.Context, batchID uuid.UUID) (*domain.BatchSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT status, count(*) FROM notifications WHERE batch_id = $1 GROUP BY status`, batchID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := &domain.BatchSummary{BatchID: batchID, StatusCounts: map[domain.Status]int{}}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		summary.StatusCounts[domain.Status(status)] = int(count)
		summary.Total += int(count)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if summary.Total == 0 {
		return nil, domain.ErrNotFound
	}
	return summary, nil
}

// List filtrelere uyan en fazla f.Limit+1 kaydı, en yeniden eskiye doğru getirir.
// Fazladan bir kayıt isteme nedeni, sonraki sayfa olup olmadığını ayrı bir sorgu atmadan anlamaktır.
func (r *NotificationRepository) List(ctx context.Context, f domain.ListFilter) ([]*domain.Notification, error) {
	var (
		conds []string
		args  []any
	)
	// bind değeri argümanlara ekler ve koşuldaki "?" yerine doğru $N numarasını koyar.
	// Kullanıcı girdisi hiçbir zaman SQL metnine eklenmez, hep parametre olarak gider.
	bind := func(cond string, arg any) {
		args = append(args, arg)
		conds = append(conds, strings.Replace(cond, "?", fmt.Sprintf("$%d", len(args)), 1))
	}

	if f.Status != nil {
		bind("status = ?", string(*f.Status))
	}
	if f.Channel != nil {
		bind("channel = ?", string(*f.Channel))
	}
	if f.BatchID != nil {
		bind("batch_id = ?::uuid", f.BatchID.String())
	}
	if f.From != nil {
		bind("created_at >= ?", *f.From)
	}
	if f.To != nil {
		bind("created_at <= ?", *f.To)
	}
	if f.Cursor != nil {
		args = append(args, f.Cursor.CreatedAt, f.Cursor.ID.String())
		conds = append(conds, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}

	q := `SELECT ` + notificationColumns + ` FROM notifications`
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	args = append(args, f.Limit+1)
	q += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
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
