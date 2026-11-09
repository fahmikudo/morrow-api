package tasksync

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/module/settings"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Handler exposes the Pepy connection routes.
type Handler struct {
	service      *Service
	integrations *settings.Repository
}

// NewHandler builds the task sync handler.
func NewHandler(service *Service, integrations *settings.Repository) *Handler {
	return &Handler{service: service, integrations: integrations}
}

// Register mounts the Pepy routes behind authentication.
func (h *Handler) Register(router chi.Router) {
	router.Post("/pepy/connect", h.connect)
	router.Post("/pepy/sync", h.sync)
	router.Post("/pepy/disconnect", h.disconnect)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	userID := httpx.UserID(r.Context())
	if !h.service.Available(r.Context(), userID) {
		httpx.Write(w, httpx.BadRequest("Pepy sync is not available"))
		return
	}

	account, err := h.service.Connect(r.Context(), userID)
	if err != nil {
		httpx.Write(w, httpx.BadRequest("Pepy login failed"))
		return
	}

	connection, err := h.integrations.Integration(r.Context(), nil, userID, PepyIntegrationID)
	if err != nil || connection == nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	connection.Enabled = true
	connection.Connected = true
	connection.AccountEmail = stringPtr(account.Email)
	connection.LastError = nil
	if err := h.integrations.SetIntegration(r.Context(), h.service.db, userID, PepyIntegrationID, *connection); err != nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	_ = httpx.JSON(w, http.StatusOK, connection)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	userID := httpx.UserID(r.Context())
	if !h.service.Available(r.Context(), userID) {
		httpx.Write(w, httpx.BadRequest("Pepy sync is not available"))
		return
	}

	connection, err := h.integrations.Integration(r.Context(), nil, userID, PepyIntegrationID)
	if err != nil || connection == nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	err = h.service.Reconcile(r.Context(), userID, true)
	now := wiretime.Now()
	if err != nil {
		message := err.Error()
		connection.LastError = &message
		_ = h.integrations.SetIntegration(r.Context(), h.service.db, userID, PepyIntegrationID, *connection)
		httpx.Write(w, httpx.BadRequest("Pepy sync failed: "+message))
		return
	}

	connection.LastSyncAt = &now
	connection.LastError = nil
	if err := h.integrations.SetIntegration(r.Context(), h.service.db, userID, PepyIntegrationID, *connection); err != nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	_ = httpx.JSON(w, http.StatusOK, connection)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	userID := httpx.UserID(r.Context())
	if err := h.service.Disconnect(r.Context(), userID); err != nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	connection, err := h.integrations.Integration(r.Context(), nil, userID, PepyIntegrationID)
	if err != nil || connection == nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}
	connection.Connected = false
	connection.AccountEmail = nil
	connection.LastError = nil
	connection.LastSyncAt = nil
	if err := h.integrations.SetIntegration(r.Context(), h.service.db, userID, PepyIntegrationID, *connection); err != nil {
		httpx.Write(w, httpx.Internal(err, nil))
		return
	}

	httpx.NoContent(w)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
