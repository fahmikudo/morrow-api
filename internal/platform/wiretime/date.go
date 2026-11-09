package wiretime

import (
	"encoding/json"
	"time"
)

const dateLayout = "2006-01-02"

// Date serialises as a zero-padded `YYYY-MM-DD` calendar date.
type Date time.Time

// ParseDate accepts only zero-padded `YYYY-MM-DD` strings.
func ParseDate(raw string) (Date, error) {
	parsed, err := time.Parse(dateLayout, raw)
	if err != nil {
		return Date{}, err
	}
	return Date(parsed), nil
}

// Time returns the date at midnight UTC.
func (d Date) Time() time.Time {
	return time.Time(d)
}

// String renders the calendar date.
func (d Date) String() string {
	return d.Time().Format(dateLayout)
}

// MarshalJSON renders `2026-11-10`.
func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON rejects any other shape.
func (d *Date) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	parsed, err := ParseDate(raw)
	if err != nil {
		return err
	}

	*d = parsed
	return nil
}
