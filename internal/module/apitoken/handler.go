package apitoken

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the token endpoints.
type Handler struct {
	service *Service
}

// NewHandler builds the token handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.service.List(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, list)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var request CreateRequest
	if err := httpx.Decode(r, &request, CreateOptions); err != nil {
		httpx.Write(w, err)
		return
	}

	created, err := h.service.Create(r.Context(), httpx.UserID(r.Context()), request)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusCreated, created)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Revoke(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id")); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}
