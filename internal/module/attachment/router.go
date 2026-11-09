package attachment

import "github.com/go-chi/chi/v5"

// Register mounts the attachment routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/attachments/config", h.config)
	router.Get("/attachments", h.list)
	router.Post("/attachments", h.prepare)
	router.Post("/attachments/{id}/complete", h.complete)
	router.Get("/attachments/{id}/url", h.url)
	router.Delete("/attachments/{id}", h.remove)
}
