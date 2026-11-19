package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// parseID URL'deki {id} parametresini UUID olarak okur; bozuksa 400 yazar.
func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a valid UUID", nil)
		return uuid.Nil, false
	}
	return id, true
}

// Get GET /api/v1/notifications/{id}
func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	n, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// Cancel POST /api/v1/notifications/{id}/cancel
func (h *NotificationHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	n, err := h.svc.Cancel(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// GetBatch GET /api/v1/batches/{id}
func (h *NotificationHandler) GetBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	summary, err := h.svc.BatchSummary(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// List GET /api/v1/notifications?status=&channel=&batch_id=&from=&to=&limit=&cursor=
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListFilter(r.URL.Query())
	if err != nil {
		h.handleError(w, err)
		return
	}
	page, err := h.svc.List(r.Context(), filter)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// parseListFilter query parametrelerini doğrular. Geçersiz değerlerin tümü tek seferde raporlanır.
func parseListFilter(q url.Values) (domain.ListFilter, error) {
	var f domain.ListFilter
	var errs []domain.FieldError
	bad := func(field, msg string) { errs = append(errs, domain.FieldError{Field: field, Message: msg}) }

	if v := q.Get("status"); v != "" {
		s := domain.Status(v)
		if s.Valid() {
			f.Status = &s
		} else {
			bad("status", "must be one of: pending, scheduled, processing, sent, failed, cancelled")
		}
	}
	if v := q.Get("channel"); v != "" {
		c := domain.Channel(v)
		if c.Valid() {
			f.Channel = &c
		} else {
			bad("channel", "must be one of: sms, email, push")
		}
	}
	if v := q.Get("batch_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.BatchID = &id
		} else {
			bad("batch_id", "must be a valid UUID")
		}
	}

	parseTime := func(field string) *time.Time {
		v := q.Get(field)
		if v == "" {
			return nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			bad(field, "must be an RFC 3339 timestamp, e.g. 2026-11-19T10:00:00Z")
			return nil
		}
		return &t
	}
	f.From = parseTime("from")
	f.To = parseTime("to")
	if f.From != nil && f.To != nil && f.From.After(*f.To) {
		bad("from", "must not be after 'to'")
	}

	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			f.Limit = n // üst sınır service katmanında uygulanır
		} else {
			bad("limit", "must be a positive integer")
		}
	}

	if len(errs) > 0 {
		return f, &domain.ValidationError{Errors: errs}
	}
	if v := q.Get("cursor"); v != "" {
		c, err := domain.DecodeCursor(v)
		if err != nil {
			return f, err
		}
		f.Cursor = c
	}
	return f, nil
}
