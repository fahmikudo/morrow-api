package auth

import (
	"net/http"
	"net/mail"
	"strings"

	"morrow-api/internal/platform/httpx"
)

// invalidCredentials is the single message both failure paths return, so a
// missing account and a wrong password stay indistinguishable.
func invalidCredentials() *httpx.Error {
	return &httpx.Error{
		Status:  http.StatusUnauthorized,
		Code:    httpx.CodeUnauthorized,
		Message: "Invalid email or password",
	}
}

// validateRegistration normalizes the sign-up body and returns the name,
// canonical email, and the first validation failure.
func validateRegistration(request RegisterRequest) (string, string, error) {
	email := strings.ToLower(strings.TrimSpace(request.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", "", httpx.BadRequest("Use a valid email")
	}

	name := strings.TrimSpace(request.Name)
	if len(name) > 80 {
		return "", "", httpx.BadRequest("Keep the name under 80 characters")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	if len(request.Password) < 8 {
		return "", "", httpx.BadRequest("Password must be at least 8 characters")
	}
	if len(request.Password) > 128 {
		return "", "", httpx.BadRequest("Keep the password under 128 characters")
	}

	return name, email, nil
}
