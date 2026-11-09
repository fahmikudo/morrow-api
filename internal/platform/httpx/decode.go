package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

// Opt distinguishes "absent" from an explicit JSON `null` on PATCH bodies.
type Opt[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON marks the field as present and remembers an explicit null.
func (o *Opt[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	o.Value = *new(T)

	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Null = true
		return nil
	}

	return json.Unmarshal(data, &o.Value)
}

// Options carries the per-payload body policy.
type Options struct {
	// Nullable lists the JSON keys that may be `null`; every other key rejects it.
	Nullable []string
	// Strict rejects unknown keys, mirroring serde's `deny_unknown_fields`.
	Strict bool
}

// Decode reads a JSON request body: it requires a body, rejects `null` for
// non-nullable keys (lowest key first), and maps every failure to a 400.
func Decode[T any](r *http.Request, dst *T, opts Options) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return BadRequest(err.Error())
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return BadRequest("Request body is required")
	}

	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return BadRequest("Request body must be a JSON object")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return BadRequest(err.Error())
	}

	allowed := make(map[string]struct{}, len(opts.Nullable))
	for _, key := range opts.Nullable {
		allowed[key] = struct{}{}
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if _, ok := allowed[key]; ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return BadRequest("Field " + key + " cannot be null")
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if opts.Strict {
		decoder.DisallowUnknownFields()
	}

	if err := decoder.Decode(dst); err != nil {
		return BadRequest(err.Error())
	}

	if decoder.More() {
		return BadRequest("Request body must contain a single JSON object")
	}

	return nil
}
