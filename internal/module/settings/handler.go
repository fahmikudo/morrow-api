package settings

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the settings, integration, and AI routes.
type Handler struct {
	service *Service
}

// NewHandler builds the settings handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Get(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, current)
}

func (h *Handler) patchIntegration(w http.ResponseWriter, r *http.Request) {
	var patch IntegrationPatch
	if err := httpx.Decode(r, &patch, IntegrationPatchOptions); err != nil {
		httpx.Write(w, err)
		return
	}

	updated, err := h.service.SetIntegrationEnabled(
		r.Context(),
		httpx.UserID(r.Context()),
		chi.URLParam(r, "id"),
		patch.Enabled,
	)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	updated, err := h.service.Connect(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	updated, err := h.service.Disconnect(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	updated, err := h.service.Sync(r.Context(), httpx.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) updateAI(w http.ResponseWriter, r *http.Request) {
	var patch UpdateAI
	if err := httpx.Decode(r, &patch, UpdateAIOptions); err != nil {
		httpx.Write(w, err)
		return
	}

	updated, err := h.service.UpdateAI(r.Context(), httpx.UserID(r.Context()), patch)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) testAI(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.TestAI(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, result)
}
