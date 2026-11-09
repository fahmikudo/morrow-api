package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// RequestIDHeader carries the correlation id in both directions.
const RequestIDHeader = "X-Request-Id"

// maxRequestIDLength bounds what a client may pin into the logs.
const maxRequestIDLength = 128

type requestIDKey struct{}

// NewRequestID returns a random UUID v4.
func NewRequestID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		panic("httpx: read random bytes: " + err.Error())
	}

	buffer[6] = (buffer[6] & 0x0f) | 0x40 // version 4
	buffer[8] = (buffer[8] & 0x3f) | 0x80 // RFC 4122 variant

	encoded := hex.EncodeToString(buffer[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// RequestIDFrom returns the id captured for this request, or "" when the
// middleware is not installed.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithRequestID keeps the client's `X-Request-Id` when it is usable, generates a
// UUID v4 otherwise, and echoes the result on the response so a client can
// correlate its call with the server log line.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if len(id) > maxRequestIDLength {
			id = ""
		}
		if id == "" {
			id = NewRequestID()
		}

		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
