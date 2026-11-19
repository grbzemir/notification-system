package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter tüm endpoint'leri tek bir http.Handler'da toplar.
func NewRouter(nh *NotificationHandler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // handler içinde panic olursa sunucu çökmesin, 500 dönsün

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/notifications", nh.Create)
		r.Post("/notifications/batch", nh.CreateBatch)
		r.Get("/notifications", nh.List)
		r.Get("/notifications/{id}", nh.Get)
		r.Post("/notifications/{id}/cancel", nh.Cancel)
		r.Get("/batches/{id}", nh.GetBatch)
	})
	return r
}
