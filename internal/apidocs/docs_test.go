package apidocs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpecServed(t *testing.T) {
	rec := httptest.NewRecorder()
	Spec(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "openapi: 3.") {
		t.Fatalf("spec must start with an openapi version, got %q", body[:20])
	}
	for _, path := range []string{"/notifications:", "/notifications/batch:", "/notifications/{id}:", "/notifications/{id}/cancel:", "/batches/{id}:", "/templates:", "/templates/{id}:"} {
		if !strings.Contains(body, "  "+path) {
			t.Errorf("spec is missing path %s", path)
		}
	}
}

func TestUIServed(t *testing.T) {
	rec := httptest.NewRecorder()
	UI(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/openapi.yaml") {
		t.Fatal("UI page must load the spec from /openapi.yaml")
	}
}
