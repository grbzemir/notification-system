package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/grbzemir/notification-system/internal/apidocs"
)

// NewRouter tüm endpoint'leri tek bir http.Handler'da toplar.
func NewRouter(nh *NotificationHandler, th *TemplateHandler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // handler içinde panic olursa sunucu çökmesin, 500 dönsün

	r.Get("/openapi.yaml", apidocs.Spec) // API şeması
	r.Get("/docs", apidocs.UI)           // Swagger arayüzü

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/notifications", nh.Create)
		r.Post("/notifications/batch", nh.CreateBatch)
		r.Get("/notifications", nh.List)
		r.Get("/notifications/{id}", nh.Get)
		r.Post("/notifications/{id}/cancel", nh.Cancel)
		r.Get("/batches/{id}", nh.GetBatch)

		r.Post("/templates", th.Create)
		r.Get("/templates", th.List)
		r.Get("/templates/{id}", th.Get)
		r.Delete("/templates/{id}", th.Delete)
	})
	return r
}
