package note

import "github.com/go-chi/chi/v5"

// Register mounts the note routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/notes", h.list)
	router.Post("/notes", h.create)
	router.Get("/notes/{id}", h.get)
	router.Patch("/notes/{id}", h.update)
	router.Delete("/notes/{id}", h.remove)
}
