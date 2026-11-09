package note

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the note routes.
type Handler struct {
	service *Service
}

// NewHandler builds the note handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	notes, err := h.service.List(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, notes)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var input New
	if err := httpx.Decode(r, &input, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	created, err := h.service.Create(r.Context(), httpx.UserID(r.Context()), input)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusCreated, created)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Get(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"))
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

	updated, err := h.service.Update(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"), patch)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Remove(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id")); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}
