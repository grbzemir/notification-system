package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTraceSkipsUntracedPathsAndPassesThrough(t *testing.T) {
	called := false
	h := Trace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	if !called || rec.Code != http.StatusTeapot {
		t.Fatalf("called=%v code=%d", called, rec.Code)
	}
	if rec.Header().Get("X-Trace-Id") != "" {
		t.Fatal("untraced paths must not get a trace id")
	}
}

// İzleme kapalıyken (varsayılan no-op sağlayıcı) bile istek bozulmadan geçmeli.
func TestTraceWithoutTracerProviderStillServes(t *testing.T) {
	h := Trace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201", rec.Code)
	}
}
