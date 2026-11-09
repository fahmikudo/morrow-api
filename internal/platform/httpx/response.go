package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON writes value with the given status and the JSON content type.
func JSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}

// NoContent writes the empty 204 response every delete and import returns.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
