package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/grbzemir/notification-system/internal/domain"
)

const maxIdempotencyKeyLength = 255

// NotificationService handler'ın çağırdığı iş mantığıdır.
type NotificationService interface {
	Create(ctx context.Context, in domain.NewNotification, idempotencyKey string) (*domain.Notification, bool, error)
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

	n, created, err := h.svc.Create(r.Context(), domain.NewNotification{
		Recipient: req.Recipient,
		Channel:   domain.Channel(req.Channel),
		Content:   req.Content,
		Priority:  domain.Priority(req.Priority),
	}, key)
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

func (h *NotificationHandler) handleError(w http.ResponseWriter, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "one or more fields are invalid", verr.Errors)
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", err.Error(), nil)
	default:
		slog.Error("create notification", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
