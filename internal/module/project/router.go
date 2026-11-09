package project

import "github.com/go-chi/chi/v5"

// Register mounts the project routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/projects", h.list)
	router.Post("/projects", h.create)
	router.Get("/projects/{id}", h.get)
	router.Patch("/projects/{id}", h.update)
	router.Delete("/projects/{id}", h.remove)
}
