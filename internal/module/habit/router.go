package habit

import "github.com/go-chi/chi/v5"

// Register mounts the habit, check-in, and skip routes.
func (h *Handler) Register(router chi.Router) {
	router.Get("/habits", h.list)
	router.Post("/habits", h.create)
	router.Get("/habits/archived", h.listArchived)
	router.Get("/habits/{id}", h.get)
	router.Patch("/habits/{id}", h.update)
	router.Delete("/habits/{id}", h.remove)
	router.Post("/habits/{id}/archive", h.archive)
	router.Post("/habits/{id}/restore", h.restore)
	router.Get("/habit-check-ins", h.listCheckIns)
	router.Put("/habits/{habitId}/check-ins/{date}", h.checkIn)
	router.Delete("/habits/{habitId}/check-ins/{date}", h.removeCheckIn)
	router.Get("/habit-skips", h.listSkips)
	router.Put("/habits/{habitId}/skips/{date}", h.skip)
	router.Delete("/habits/{habitId}/skips/{date}", h.removeSkip)
}
