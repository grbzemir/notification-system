package handler

import (
	"encoding/json"
	"net/http"

	"github.com/grbzemir/notification-system/internal/domain"
)

// 1000 adet 10.000 karakterlik e-posta yaklaşık 10 MB eder; bunun üstüne pay bırakıyoruz.
const maxBatchBodyBytes = 20 << 20

type batchRequest struct {
	Notifications []createRequest `json:"notifications"`
}

// CreateBatch POST /api/v1/notifications/batch
func (h *NotificationHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBatchBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req batchRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error(), nil)
		return
	}

	items := make([]domain.NewNotification, len(req.Notifications))
	for i, c := range req.Notifications {
		items[i] = c.toDomain()
	}

	batchID, count, err := h.svc.CreateBatch(r.Context(), items)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"batch_id": batchID, "count": count})
}
