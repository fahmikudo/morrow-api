package search

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes GET /search.
type Handler struct {
	service *Service
}

// NewHandler builds the search handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	results, err := h.service.Search(r.Context(), httpx.UserID(r.Context()), r.URL.Query().Get("q"))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, results)
}

// Register mounts the search route.
func (h *Handler) Register(router chi.Router) {
	router.Get("/search", h.search)
}
