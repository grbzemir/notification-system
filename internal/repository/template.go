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
	templateColumns         = `id::text, name, body, created_at, updated_at`
	templateNameUniqueIndex = "uq_templates_name"
	maxListedTemplates      = 200
)

// TemplateRepository şablonları PostgreSQL'de saklar.
type TemplateRepository struct {
	pool *pgxpool.Pool
}

func NewTemplateRepository(pool *pgxpool.Pool) *TemplateRepository {
	return &TemplateRepository{pool: pool}
}

func scanTemplate(row rowScanner) (*domain.Template, error) {
	var (
		t  domain.Template
		id string
	)
	if err := row.Scan(&id, &t.Name, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTemplateNotFound
		}
		return nil, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	t.ID = parsed
	t.Variables = domain.ExtractVariables(t.Body)
	return &t, nil
}

// Create şablonu ekler. Ad daha önce kullanılmışsa domain.ErrTemplateNameTaken döner.
func (r *TemplateRepository) Create(ctx context.Context, t *domain.Template) error {
	const q = `INSERT INTO templates (id, name, body) VALUES ($1, $2, $3) RETURNING created_at, updated_at`

	err := r.pool.QueryRow(ctx, q, t.ID.String(), t.Name, t.Body).Scan(&t.CreatedAt, &t.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == templateNameUniqueIndex {
		return domain.ErrTemplateNameTaken
	}
	return err
}

func (r *TemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Template, error) {
	return scanTemplate(r.pool.QueryRow(ctx, `SELECT `+templateColumns+` FROM templates WHERE id = $1`, id.String()))
}

// List şablonları ada göre sıralı döner (en fazla maxListedTemplates adet).
func (r *TemplateRepository) List(ctx context.Context) ([]*domain.Template, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+templateColumns+` FROM templates ORDER BY name LIMIT $1`, maxListedTemplates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Delete şablonu siler. Daha önce bu şablondan üretilmiş bildirimler etkilenmez (içerik bildirime yazılmıştır).
func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM templates WHERE id = $1`, id.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTemplateNotFound
	}
	return nil
}
