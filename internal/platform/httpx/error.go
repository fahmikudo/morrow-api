// Package httpx holds the HTTP primitives every feature shares: the error
// envelope, the JSON body decoder, middleware, and the router assembly.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Code is the stable, machine-readable half of the error envelope.
type Code string

const (
	CodeValidation   Code = "validation_error"
	CodeUnauthorized Code = "unauthorized"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeUnavailable  Code = "unavailable"
	CodeInternal     Code = "internal_error"
)

// Error is the only error type handlers return. Status and Code are derived
// from the constructor, never set by feature code.
type Error struct {
	Status  int
	Code    Code
	Message string
}

// Error implements the error interface; the message is what clients see.
func (e *Error) Error() string {
	return e.Message
}

// BadRequest reports a 400 validation failure.
func BadRequest(message string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeValidation, Message: message}
}

// Unauthorized reports a missing or unusable bearer credential.
func Unauthorized() *Error {
	return &Error{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: "Unauthorized"}
}

// NotFound reports an unknown id or route.
func NotFound(message string) *Error {
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: message}
}

// Conflict reports a uniqueness clash.
func Conflict(message string) *Error {
	return &Error{Status: http.StatusConflict, Code: CodeConflict, Message: message}
}

// Unavailable reports a feature that is not configured for this deployment.
func Unavailable(message string) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: message}
}

// Internal logs the cause and hides it behind the standard message.
func Internal(err error, log *slog.Logger) *Error {
	if log != nil {
		log.Error("internal error", "error", err)
	}

	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternal,
		Message: "Internal server error",
	}
}

// Map passes an *Error through unchanged and turns anything else into the
// logged 500 envelope. A nil error stays nil.
func Map(err error, log *slog.Logger) error {
	if err == nil {
		return nil
	}

	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	return Internal(err, log)
}

type errorBody struct {
	Error string `json:"error"`
	Code  Code   `json:"code"`
}

// Write emits the error envelope. Anything that is not an *Error is treated as
// an internal failure so no detail leaks.
func Write(w http.ResponseWriter, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = &Error{
			Status:  http.StatusInternalServerError,
			Code:    CodeInternal,
			Message: "Internal server error",
		}
	}

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(apiErr.Status)

	_ = json.NewEncoder(w).Encode(errorBody{Error: apiErr.Message, Code: apiErr.Code})
}
