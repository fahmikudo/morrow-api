package httpx

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// RouterOptions describes the assembled application router.
type RouterOptions struct {
	CORSOrigins   []string
	Logger        *slog.Logger
	BodyLimit     int64
	Authenticator Authenticator
	// Public registers the /api/v1 routes that need no credential (login only).
	Public func(router chi.Router)
	// Protected registers every /api/v1 route behind the bearer credential.
	Protected func(router chi.Router)
}

// NewRouter assembles middleware, the health probe, and the authenticated API
// tree. Route handlers only ever see a resolved identity from `Protected`.
func NewRouter(opts RouterOptions) http.Handler {
	router := chi.NewRouter()

	router.Use(WithRequestID)
	router.Use(middleware.RealIP)
	router.Use(RequestLogger(opts.Logger))
	router.Use(Recoverer(opts.Logger))

	if opts.BodyLimit > 0 {
		router.Use(BodyLimit(opts.BodyLimit))
	}

	router.Use(cors.Handler(cors.Options{
		AllowedOrigins: opts.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type", RequestIDHeader},
		ExposedHeaders: []string{RequestIDHeader},
		MaxAge:         300,
	}))

	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		Write(w, NotFound("Route not found"))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	router.Route("/api/v1", func(api chi.Router) {
		if opts.Public != nil {
			opts.Public(api)
		}
		if opts.Protected != nil {
			api.Group(func(protected chi.Router) {
				protected.Use(Authenticate(opts.Authenticator))
				opts.Protected(protected)
			})
		}
	})

	return router
}
