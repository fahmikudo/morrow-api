package auth

import (
	"net/http"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes the four auth endpoints.
type Handler struct {
	service *Service
}

// NewHandler builds the auth handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body RegisterRequest
	if err := httpx.Decode(r, &body, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	token, current, err := h.service.Register(r.Context(), body)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusCreated, LoginResponse{Token: token, Profile: current})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if err := httpx.Decode(r, &request, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	token, current, err := h.service.Login(r.Context(), request)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, LoginResponse{Token: token, Profile: current})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := httpx.SessionID(r.Context())
	if err := h.service.Logout(r.Context(), sessionID); err != nil {
		httpx.Write(w, err)
		return
	}

	httpx.NoContent(w)
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.SessionProfile(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, SessionResponse{Profile: *current})
}
