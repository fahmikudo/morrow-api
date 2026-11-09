package calendar

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the Google OAuth and sync routes.
type Handler struct {
	service *Service
}

// NewHandler builds the calendar handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterPublic mounts the OAuth callback, which Google calls without a
// bearer token; the state row identifies the user.
func (h *Handler) RegisterPublic(router chi.Router) {
	router.Get("/integrations/google-calendar/oauth/callback", h.callback)
}

// Register mounts the authenticated Google Calendar routes.
func (h *Handler) Register(router chi.Router) {
	router.Post("/google-calendar/oauth/start", h.start)
	router.Get("/google-calendar/calendars", h.calendars)
	router.Put("/google-calendar/calendar", h.selectCalendar)
	router.Post("/google-calendar/sync", h.sync)
	router.Post("/google-calendar/disconnect", h.disconnect)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	authorizationURL, err := h.service.Start(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, map[string]string{"authorizationUrl": authorizationURL})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	target := h.service.cfg.CORSOrigins
	base := "http://localhost:5173"
	if len(target) > 0 {
		base = strings.TrimRight(target[0], "/")
	}

	if value := strings.TrimSpace(query.Get("error")); value != "" {
		redirect(w, r, base+"/settings?google=error&reason="+url.QueryEscape(value))
		return
	}

	_, userID, err := h.service.Callback(r.Context(), query.Get("code"), query.Get("state"))
	if err != nil {
		redirect(w, r, base+"/settings?google=error&reason="+url.QueryEscape(err.Error()))
		return
	}

	syncStatus := "connected"
	if err := h.service.SyncUser(r.Context(), userID); err != nil {
		syncStatus = "sync-error"
	}

	redirect(w, r, base+"/settings?google="+syncStatus)
}

func (h *Handler) calendars(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Calendars(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) selectCalendar(w http.ResponseWriter, r *http.Request) {
	var body SelectRequest
	if err := httpx.Decode(r, &body, httpx.Options{Strict: true}); err != nil {
		httpx.Write(w, err)
		return
	}

	connection, err := h.service.SelectCalendar(
		r.Context(), httpx.UserID(r.Context()), body.CalendarID,
	)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	// Import the new calendar without failing the selection if Google is slow.
	_ = h.service.SyncUser(r.Context(), httpx.UserID(r.Context()))
	_ = httpx.JSON(w, http.StatusOK, connection)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	userID := httpx.UserID(r.Context())
	if err := h.service.SyncUser(r.Context(), userID); err != nil {
		httpx.Write(w, err)
		return
	}

	connection, err := h.service.Connection(r.Context(), userID)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, connection)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Disconnect(r.Context(), httpx.UserID(r.Context())); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}

func redirect(w http.ResponseWriter, r *http.Request, target string) {
	http.Redirect(w, r, target, http.StatusFound)
}
