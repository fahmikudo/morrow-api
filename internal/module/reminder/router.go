package reminder

import "github.com/go-chi/chi/v5"

// Register mounts the reminder and push routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/reminders/settings", h.settings)
	router.Patch("/reminders/settings", h.updateSettings)
	router.Get("/reminders", h.today)
	router.Post("/reminders/{id}/dismiss", h.dismiss)
	router.Get("/push/config", h.pushConfig)
	router.Post("/push/subscriptions", h.subscribe)
	router.Delete("/push/subscriptions", h.unsubscribe)
}
