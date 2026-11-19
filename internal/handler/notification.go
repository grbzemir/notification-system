package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

const maxIdempotencyKeyLength = 255

// NotificationService handler'ın çağırdığı iş mantığıdır.
type NotificationService interface {
	Create(ctx context.Context, in domain.NewNotification, idempotencyKey string) (*domain.Notification, bool, error)
	CreateBatch(ctx context.Context, items []domain.NewNotification) (uuid.UUID, int, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	Cancel(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	BatchSummary(ctx context.Context, batchID uuid.UUID) (*domain.BatchSummary, error)
	List(ctx context.Context, f domain.ListFilter) (*domain.Page, error)
}

type NotificationHandler struct {
	svc NotificationService
}

func NewNotificationHandler(svc NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

type createRequest struct {
	Recipient string `json:"recipient"`
	Channel   string `json:"channel"`
	Content   string `json:"content"`
	Priority  string `json:"priority"`
}

func (c createRequest) toDomain() domain.NewNotification {
	return domain.NewNotification{
		Recipient: c.Recipient,
		Channel:   domain.Channel(c.Channel),
		Content:   c.Content,
		Priority:  domain.Priority(c.Priority),
	}
}

// Create POST /api/v1/notifications
// Opsiyonel "Idempotency-Key" header'ı tekrar eden istekleri tek kayda indirger.
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) > maxIdempotencyKeyLength {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be at most 255 characters", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // en fazla 1 MB gövde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error(), nil)
		return
	}

	n, created, err := h.svc.Create(r.Context(), req.toDomain(), key)
	if err != nil {
		h.handleError(w, err)
		return
	}

	if !created {
		w.Header().Set("Idempotent-Replayed", "true")
		writeJSON(w, http.StatusOK, n)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

// handleError servis katmanından gelen hataları HTTP cevabına çevirir.
func (h *NotificationHandler) handleError(w http.ResponseWriter, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "one or more fields are invalid", verr.Errors)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, domain.ErrNotCancellable), errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, domain.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error(), nil)
	default:
		slog.Error("request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
