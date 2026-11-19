package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// TemplateStore şablon servisinin ihtiyaç duyduğu depolama işlemleridir (consumer-defined interface).
type TemplateStore interface {
	Create(ctx context.Context, t *domain.Template) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Template, error)
	List(ctx context.Context) ([]*domain.Template, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type TemplateService struct {
	repo TemplateStore
}

func NewTemplateService(repo TemplateStore) *TemplateService {
	return &TemplateService{repo: repo}
}

func (s *TemplateService) Create(ctx context.Context, in domain.NewTemplate) (*domain.Template, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	t := &domain.Template{
		ID:        uuid.New(),
		Name:      strings.TrimSpace(in.Name),
		Body:      in.Body,
		Variables: domain.ExtractVariables(in.Body),
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TemplateService) Get(ctx context.Context, id uuid.UUID) (*domain.Template, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *TemplateService) List(ctx context.Context) ([]*domain.Template, error) {
	return s.repo.List(ctx)
}

func (s *TemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// Render bildirim oluşturma sırasında kullanılır. Şablon yoksa bu bir doğrulama hatasıdır (template_id alanı), 404 değil:
// istemcinin gönderdiği bildirim isteği geçersizdir.
func (s *TemplateService) Render(ctx context.Context, id uuid.UUID, vars map[string]string) (string, error) {
	t, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, domain.ErrTemplateNotFound) {
		return "", &domain.ValidationError{Errors: []domain.FieldError{{Field: "template_id", Message: "template not found"}}}
	}
	if err != nil {
		return "", err
	}
	return t.Render(vars)
}
