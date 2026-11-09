package area

import "github.com/go-chi/chi/v5"

// Register mounts the area routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/areas", h.list)
	router.Post("/areas", h.create)
	router.Get("/areas/{id}", h.get)
	router.Patch("/areas/{id}", h.update)
	router.Delete("/areas/{id}", h.remove)
}
