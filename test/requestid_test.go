package apitest

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"morrow-api/internal/platform/httpx"
)

var uuidV4 = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestRequestIDIsCapturedOrGenerated(t *testing.T) {
	app := New(t)

	t.Run("client value is kept and echoed", func(t *testing.T) {
		status, headers := app.DoWithHeaders(
			http.MethodGet, "/healthz", "", "",
			map[string]string{httpx.RequestIDHeader: "client-trace-123"},
		)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if got := headers.Get(httpx.RequestIDHeader); got != "client-trace-123" {
			t.Errorf("X-Request-Id = %q, want %q", got, "client-trace-123")
		}
	})

	t.Run("missing header generates a UUID v4", func(t *testing.T) {
		_, headers := app.DoWithHeaders(http.MethodGet, "/healthz", "", "", nil)
		generated := headers.Get(httpx.RequestIDHeader)
		if !uuidV4.MatchString(generated) {
			t.Fatalf("X-Request-Id = %q, want a UUID v4", generated)
		}

		_, headers = app.DoWithHeaders(http.MethodGet, "/healthz", "", "", nil)
		next := headers.Get(httpx.RequestIDHeader)
		if next == generated {
			t.Errorf("two requests reused %q", generated)
		}
		if !uuidV4.MatchString(next) {
			t.Errorf("X-Request-Id = %q, want a UUID v4", next)
		}
	})

	t.Run("blank and oversized values fall back to a UUID", func(t *testing.T) {
		for _, value := range []string{"   ", strings.Repeat("a", 129)} {
			_, headers := app.DoWithHeaders(
				http.MethodGet, "/healthz", "", "",
				map[string]string{httpx.RequestIDHeader: value},
			)
			if got := headers.Get(httpx.RequestIDHeader); !uuidV4.MatchString(got) {
				t.Errorf("value %q produced X-Request-Id %q, want a UUID v4", value, got)
			}
		}
	})

	t.Run("error responses carry it too", func(t *testing.T) {
		status, headers := app.DoWithHeaders(
			http.MethodGet, "/api/v1/tasks", "", "",
			map[string]string{httpx.RequestIDHeader: "client-trace-401"},
		)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
		if got := headers.Get(httpx.RequestIDHeader); got != "client-trace-401" {
			t.Errorf("X-Request-Id = %q, want %q", got, "client-trace-401")
		}
	})
}

func TestRequestIDMiddlewareContextAndEcho(t *testing.T) {
	var seen string

	handler := httpx.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = httpx.RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(httpx.RequestIDHeader, "  trace-abc  ")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if seen != "trace-abc" {
		t.Errorf("context id = %q, want %q", seen, "trace-abc")
	}
	if got := recorder.Header().Get(httpx.RequestIDHeader); got != "trace-abc" {
		t.Errorf("response id = %q, want %q", got, "trace-abc")
	}

	// Without the middleware the context accessor is empty, never a panic.
	if got := httpx.RequestIDFrom(request.Context()); got != "" {
		t.Errorf("RequestIDFrom without middleware = %q, want empty", got)
	}
}
