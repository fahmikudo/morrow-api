package settings

import "github.com/go-chi/chi/v5"

// Register mounts the settings, integration, and AI routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/settings", h.get)
	router.Patch("/integrations/{id}", h.patchIntegration)
	router.Post("/integrations/{id}/connect", h.connect)
	router.Post("/integrations/{id}/disconnect", h.disconnect)
	router.Post("/integrations/{id}/sync", h.sync)
	router.Patch("/ai", h.updateAI)
	router.Post("/ai/test", h.testAI)
}
