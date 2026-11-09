package snapshot

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes GET /snapshot.
type Handler struct {
	service *Service
}

// NewHandler builds the snapshot handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Snapshot(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, current)
}

// Register mounts the snapshot route.
func (h *Handler) Register(router chi.Router) {
	router.Get("/snapshot", h.get)
}
