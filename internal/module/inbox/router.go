package inbox

import "github.com/go-chi/chi/v5"

// Register mounts the inbox routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/inbox", h.list)
	router.Post("/inbox", h.create)
	router.Get("/inbox/archived", h.listArchived)
	router.Get("/inbox/{id}", h.get)
	router.Patch("/inbox/{id}", h.update)
	router.Delete("/inbox/{id}", h.remove)
	router.Post("/inbox/{id}/archive", h.archive)
	router.Post("/inbox/{id}/restore", h.restore)
}
