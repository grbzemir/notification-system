package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// TemplateService handler'ın şablon işlemleri için ihtiyaç duyduğu servis arayüzüdür.
type TemplateService interface {
	Create(ctx context.Context, in domain.NewTemplate) (*domain.Template, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Template, error)
	List(ctx context.Context) ([]*domain.Template, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// TemplateRenderer bildirim oluştururken şablondan içerik üretir.
type TemplateRenderer interface {
	Render(ctx context.Context, id uuid.UUID, vars map[string]string) (string, error)
}

type TemplateHandler struct {
	svc TemplateService
}

func NewTemplateHandler(svc TemplateService) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

type createTemplateRequest struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createTemplateRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error(), nil)
		return
	}

	t, err := h.svc.Create(r.Context(), domain.NewTemplate{Name: req.Name, Body: req.Body})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *TemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	t, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if items == nil {
		items = []*domain.Template{} // JSON'da null yerine []
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveContent istek bir şablon kullanıyorsa bildirim içeriğini şablondan üretir.
// content ile template_id birlikte verilemez.
func (h *NotificationHandler) resolveContent(ctx context.Context, c *createRequest) error {
	if c.TemplateID == nil {
		return nil
	}
	if c.Content != "" {
		return &domain.ValidationError{Errors: []domain.FieldError{{Field: "content", Message: "must not be set together with template_id"}}}
	}
	if h.templates == nil {
		return &domain.ValidationError{Errors: []domain.FieldError{{Field: "template_id", Message: "templates are not available"}}}
	}
	content, err := h.templates.Render(ctx, *c.TemplateID, c.Variables)
	if err != nil {
		return err
	}
	c.Content = content
	return nil
}

// prefixFields toplu istekte hata alan adlarını "notifications[i]." ile öne ekler.
func prefixFields(err error, index int) error {
	verr, ok := err.(*domain.ValidationError)
	if !ok {
		return err
	}
	out := make([]domain.FieldError, len(verr.Errors))
	for i, fe := range verr.Errors {
		out[i] = domain.FieldError{Field: fmt.Sprintf("notifications[%d].%s", index, fe.Field), Message: fe.Message}
	}
	return &domain.ValidationError{Errors: out}
}
