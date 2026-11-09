package attachment

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the attachment routes.
type Handler struct {
	service *Service
}

// NewHandler builds the attachment handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) config(w http.ResponseWriter, _ *http.Request) {
	_ = httpx.JSON(w, http.StatusOK, h.service.Config())
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	attachments, err := h.service.List(
		r.Context(),
		httpx.UserID(r.Context()),
		query.Get("entityType"),
		query.Get("entityId"),
	)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, attachments)
}

func (h *Handler) prepare(w http.ResponseWriter, r *http.Request) {
	var input Prepare
	if err := httpx.Decode(r, &input, httpx.Options{Strict: true}); err != nil {
		httpx.Write(w, err)
		return
	}

	ticket, err := h.service.Prepare(r.Context(), httpx.UserID(r.Context()), input)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusCreated, ticket)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	attachment, err := h.service.Complete(
		r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"),
	)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, attachment)
}

func (h *Handler) url(w http.ResponseWriter, r *http.Request) {
	inline := r.URL.Query().Get("inline") == "true"
	response, err := h.service.URL(
		r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"), inline,
	)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, response)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(
		r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"),
	); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}
