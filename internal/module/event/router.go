package event

import "github.com/go-chi/chi/v5"

// Register mounts the event routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/events", h.list)
	router.Post("/events", h.create)
	router.Get("/events/{id}", h.get)
	router.Patch("/events/{id}", h.update)
	router.Delete("/events/{id}", h.remove)
}
