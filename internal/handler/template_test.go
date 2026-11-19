package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

type fakeRenderer struct {
	content string
	err     error
	gotID   uuid.UUID
	gotVars map[string]string
}

func (f *fakeRenderer) Render(_ context.Context, id uuid.UUID, vars map[string]string) (string, error) {
	f.gotID, f.gotVars = id, vars
	return f.content, f.err
}

func TestResolveContentWithoutTemplateIsNoop(t *testing.T) {
	h := NewNotificationHandler(nil)
	req := createRequest{Content: "düz metin"}

	if err := h.resolveContent(context.Background(), &req); err != nil {
		t.Fatal(err)
	}
	if req.Content != "düz metin" {
		t.Fatalf("content changed: %q", req.Content)
	}
}

func TestResolveContentRendersTemplate(t *testing.T) {
	id := uuid.New()
	r := &fakeRenderer{content: "Merhaba Ada"}
	h := NewNotificationHandler(nil).WithTemplates(r)
	req := createRequest{TemplateID: &id, Variables: map[string]string{"name": "Ada"}}

	if err := h.resolveContent(context.Background(), &req); err != nil {
		t.Fatal(err)
	}
	if req.Content != "Merhaba Ada" || r.gotID != id || r.gotVars["name"] != "Ada" {
		t.Fatalf("content=%q id=%v vars=%v", req.Content, r.gotID, r.gotVars)
	}
}

func TestResolveContentRejectsContentWithTemplate(t *testing.T) {
	id := uuid.New()
	h := NewNotificationHandler(nil).WithTemplates(&fakeRenderer{})
	req := createRequest{Content: "x", TemplateID: &id}

	err := h.resolveContent(context.Background(), &req)

	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Errors[0].Field != "content" {
		t.Fatalf("want content validation error, got %v", err)
	}
}

func TestResolveContentTemplatesDisabled(t *testing.T) {
	id := uuid.New()
	h := NewNotificationHandler(nil)
	req := createRequest{TemplateID: &id}

	err := h.resolveContent(context.Background(), &req)

	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Errors[0].Field != "template_id" {
		t.Fatalf("want template_id validation error, got %v", err)
	}
}

func TestResolveContentPropagatesRenderError(t *testing.T) {
	id := uuid.New()
	boom := errors.New("boom")
	h := NewNotificationHandler(nil).WithTemplates(&fakeRenderer{err: boom})
	req := createRequest{TemplateID: &id}

	if err := h.resolveContent(context.Background(), &req); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestPrefixFields(t *testing.T) {
	in := &domain.ValidationError{Errors: []domain.FieldError{{Field: "variables.name", Message: "m"}}}

	err := prefixFields(in, 3)

	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Errors[0].Field != "notifications[3].variables.name" {
		t.Fatalf("got %v", err)
	}
	other := errors.New("x")
	if prefixFields(other, 0) != other {
		t.Fatal("non-validation errors must pass through unchanged")
	}
}
