package profile

import (
	"net/http"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes GET and PATCH /profile.
type Handler struct {
	service *Service
}

// NewHandler builds the profile handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register mounts the profile routes on the authenticated group.

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Get(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, current)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var patch Update
	if err := httpx.Decode(r, &patch, UpdateOptions); err != nil {
		httpx.Write(w, err)
		return
	}

	updated, err := h.service.Update(r.Context(), httpx.UserID(r.Context()), patch)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}
