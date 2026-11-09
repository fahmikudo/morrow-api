package apitoken

import "github.com/go-chi/chi/v5"

// Register mounts the personal access token routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/api-tokens", h.list)
	router.Post("/api-tokens", h.create)
	router.Delete("/api-tokens/{id}", h.revoke)
}
