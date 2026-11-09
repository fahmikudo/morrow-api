package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Authenticator resolves a bearer token to an identity. Personal access tokens
// return a nil session id.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (userID string, sessionID *string, err error)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	written, err := r.ResponseWriter.Write(data)
	r.bytes += written
	return written, err
}

func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RequestLogger emits one structured line per request.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &responseRecorder{ResponseWriter: w}
			holder := &logIdentity{}
			started := time.Now()

			next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), logIdentityKey{}, holder)))

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}

			attributes := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", recorder.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
			}
			if holder.userID != "" {
				attributes = append(attributes, "user", holder.userID)
			}

			log.Info("request", attributes...)
		})
	}
}

// Recoverer turns a panic into the standard 500 envelope and logs the stack.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				log.Error("panic recovered",
					"error", recovered,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				Write(w, &Error{
					Status:  http.StatusInternalServerError,
					Code:    CodeInternal,
					Message: "Internal server error",
				})
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps the request body every endpoint accepts.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate requires `Authorization: Bearer <token>` and stores the identity.
func Authenticate(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			token, found := strings.CutPrefix(header, "Bearer ")
			token = strings.TrimSpace(token)
			if !found || token == "" {
				Write(w, Unauthorized())
				return
			}

			userID, sessionID, err := authenticator.Authenticate(r.Context(), token)
			if err != nil {
				Write(w, err)
				return
			}

			if holder, ok := r.Context().Value(logIdentityKey{}).(*logIdentity); ok {
				holder.userID = userID
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), userID, sessionID)))
		})
	}
}
