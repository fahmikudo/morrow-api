package auth

import "github.com/go-chi/chi/v5"

// RegisterPublic mounts the endpoints that need no credential.
func (h *Handler) RegisterPublic(router chi.Router) {
	router.Post("/auth/login", h.login)
	router.Post("/auth/register", h.register)
}

// Register mounts the authenticated auth routes.
func (h *Handler) Register(router chi.Router) {
	router.Post("/auth/logout", h.logout)
	router.Get("/auth/session", h.session)
}
