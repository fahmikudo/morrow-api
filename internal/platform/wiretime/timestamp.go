// Package wiretime holds the JSON shapes of every timestamp and date on the wire.
package wiretime

import (
	"encoding/json"
	"time"
)

const timestampLayout = "2006-01-02T15:04:05.000Z"

// Timestamp serialises as RFC 3339 with millisecond precision and a `Z`
// suffix, matching the browser's `Date#toISOString`.
type Timestamp time.Time

// Now returns the current instant in UTC; the single clock for the service layer.
func Now() Timestamp {
	return Timestamp(time.Now().UTC())
}

// Time returns the underlying instant.
func (t Timestamp) Time() time.Time {
	return time.Time(t)
}

// MarshalJSON renders `2026-11-08T03:15:27.412Z`.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.Time().UTC().Format(timestampLayout) + `"`), nil
}

// ParseTimestamp accepts any RFC 3339 timestamp with or without a fraction.
func ParseTimestamp(raw string) (Timestamp, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return Timestamp{}, err
	}

	return Timestamp(parsed.UTC()), nil
}

// UnmarshalJSON accepts any RFC 3339 timestamp with or without a fraction.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	parsed, err := ParseTimestamp(raw)
	if err != nil {
		return err
	}

	*t = parsed
	return nil
}
