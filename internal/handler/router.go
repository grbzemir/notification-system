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
	})
	return r
}
