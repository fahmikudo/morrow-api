package profile

import (
	"github.com/go-chi/chi/v5"
)

// Register mounts the profile routes on the authenticated group.
func (h *Handler) Register(router chi.Router) {
	router.Get("/profile", h.get)
	router.Patch("/profile", h.update)
}
