package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/grbzemir/notification-system/internal/domain"
)

// NotificationService handler'ın çağırdığı iş mantığıdır.
type NotificationService interface {
	Create(ctx context.Context, in domain.NewNotification) (*domain.Notification, error)
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
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // en fazla 1 MB gövde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error(), nil)
		return
	}

	n, err := h.svc.Create(r.Context(), domain.NewNotification{
		Recipient: req.Recipient,
		Channel:   domain.Channel(req.Channel),
		Content:   req.Content,
		Priority:  domain.Priority(req.Priority),
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (h *NotificationHandler) handleError(w http.ResponseWriter, err error) {
	var verr *domain.ValidationError
	if errors.As(err, &verr) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "one or more fields are invalid", verr.Errors)
		return
	}
	slog.Error("create notification", "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
}
