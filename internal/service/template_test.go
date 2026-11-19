package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

type fakeTemplateStore struct {
	items map[uuid.UUID]*domain.Template
}

func newFakeTemplateStore() *fakeTemplateStore {
	return &fakeTemplateStore{items: map[uuid.UUID]*domain.Template{}}
}

func (f *fakeTemplateStore) Create(_ context.Context, t *domain.Template) error {
	for _, e := range f.items {
		if e.Name == t.Name {
			return domain.ErrTemplateNameTaken
		}
	}
	f.items[t.ID] = t
	return nil
}

func (f *fakeTemplateStore) GetByID(_ context.Context, id uuid.UUID) (*domain.Template, error) {
	if t, ok := f.items[id]; ok {
		return t, nil
	}
	return nil, domain.ErrTemplateNotFound
}

func (f *fakeTemplateStore) List(context.Context) ([]*domain.Template, error) {
	out := make([]*domain.Template, 0, len(f.items))
	for _, t := range f.items {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeTemplateStore) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrTemplateNotFound
	}
	delete(f.items, id)
	return nil
}

func TestTemplateServiceCreateDerivesVariables(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())

	tpl, err := svc.Create(context.Background(), domain.NewTemplate{Name: "  otp  ", Body: "Kod {{code}} - {{name}}"})
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Name != "otp" {
		t.Errorf("name should be trimmed, got %q", tpl.Name)
	}
	if len(tpl.Variables) != 2 || tpl.Variables[0] != "code" || tpl.Variables[1] != "name" {
		t.Errorf("variables = %v", tpl.Variables)
	}
	if tpl.ID == uuid.Nil {
		t.Error("id must be generated")
	}
}

func TestTemplateServiceCreateValidates(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())

	_, err := svc.Create(context.Background(), domain.NewTemplate{})

	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestTemplateServiceDuplicateName(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())
	in := domain.NewTemplate{Name: "otp", Body: "x"}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Create(context.Background(), in)

	if !errors.Is(err, domain.ErrTemplateNameTaken) {
		t.Fatalf("want ErrTemplateNameTaken, got %v", err)
	}
}

func TestTemplateServiceRender(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())
	tpl, _ := svc.Create(context.Background(), domain.NewTemplate{Name: "otp", Body: "Merhaba {{name}}"})

	got, err := svc.Render(context.Background(), tpl.ID, map[string]string{"name": "Ada"})
	if err != nil || got != "Merhaba Ada" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Bilinmeyen şablon bildirim isteğinde 404 değil, template_id alanında doğrulama hatasıdır.
func TestTemplateServiceRenderUnknownTemplateIsValidationError(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())

	_, err := svc.Render(context.Background(), uuid.New(), nil)

	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Errors[0].Field != "template_id" {
		t.Fatalf("want template_id validation error, got %v", err)
	}
}

func TestTemplateServiceRenderMissingVariable(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())
	tpl, _ := svc.Create(context.Background(), domain.NewTemplate{Name: "otp", Body: "{{a}}"})

	_, err := svc.Render(context.Background(), tpl.ID, map[string]string{})

	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Errors[0].Field != "variables.a" {
		t.Fatalf("want variables.a error, got %v", err)
	}
}

func TestTemplateServiceDeleteAndGet(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateStore())
	tpl, _ := svc.Create(context.Background(), domain.NewTemplate{Name: "n", Body: "b"})

	if err := svc.Delete(context.Background(), tpl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), tpl.ID); !errors.Is(err, domain.ErrTemplateNotFound) {
		t.Fatalf("want not found after delete, got %v", err)
	}
	if err := svc.Delete(context.Background(), tpl.ID); !errors.Is(err, domain.ErrTemplateNotFound) {
		t.Fatalf("second delete should be not found, got %v", err)
	}
}
