package reminder

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the reminder and push routes.
type Handler struct {
	service *Service
}

// NewHandler builds the reminder handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Settings(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, current)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var patch SettingsUpdate
	if err := httpx.Decode(r, &patch, SettingsUpdateOptions); err != nil {
		httpx.Write(w, err)
		return
	}

	updated, err := h.service.UpdateSettings(r.Context(), httpx.UserID(r.Context()), patch)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) today(w http.ResponseWriter, r *http.Request) {
	reminders, err := h.service.Today(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, reminders)
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	err := h.service.Dismiss(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}

func (h *Handler) pushConfig(w http.ResponseWriter, r *http.Request) {
	_ = httpx.JSON(w, http.StatusOK, h.service.PushConfig())
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	var body SubscribeRequest
	if err := httpx.Decode(r, &body, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	var userAgent *string
	if value := r.UserAgent(); value != "" {
		userAgent = &value
	}

	if err := h.service.Subscribe(r.Context(), httpx.UserID(r.Context()), body, userAgent); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var body UnsubscribeRequest
	if err := httpx.Decode(r, &body, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	if err := h.service.Unsubscribe(r.Context(), httpx.UserID(r.Context()), body.Endpoint); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}
