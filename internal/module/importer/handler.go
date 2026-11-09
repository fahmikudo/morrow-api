package importer

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes POST /import.
type Handler struct {
	service *Service
}

// NewHandler builds the import handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) importData(w http.ResponseWriter, r *http.Request) {
	var payload Database
	if err := httpx.Decode(r, &payload, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	if err := h.service.Import(r.Context(), httpx.UserID(r.Context()), payload); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}

// Register mounts the import route.
func (h *Handler) Register(router chi.Router) {
	router.Post("/import", h.importData)
}
