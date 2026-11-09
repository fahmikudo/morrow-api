package task

import "github.com/go-chi/chi/v5"

// Register mounts the task routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/tasks", h.list)
	router.Post("/tasks", h.create)
	router.Get("/tasks/{id}", h.get)
	router.Patch("/tasks/{id}", h.update)
	router.Delete("/tasks/{id}", h.remove)
}
