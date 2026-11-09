package assistant

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"morrow-api/internal/platform/httpx"
)

// Handler exposes POST /assistant.
type Handler struct {
	service *Service
}

// NewHandler builds the assistant handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request) {
	var request Request
	if err := httpx.Decode(r, &request, httpx.Options{}); err != nil {
		httpx.Write(w, err)
		return
	}

	response, err := h.service.Reply(r.Context(), httpx.UserID(r.Context()), request)
	if err != nil {
		httpx.Write(w, err)
		return
	}

	_ = httpx.JSON(w, http.StatusOK, response)
}

// Register mounts the assistant route.
func (h *Handler) Register(router chi.Router) {
	router.Post("/assistant", h.reply)
}
