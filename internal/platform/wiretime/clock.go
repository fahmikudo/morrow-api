package wiretime

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// Clock is a time-of-day with minute precision. PostgreSQL stores it as `time`;
// the wire and every service keep using the `HH:mm` string.
type Clock string

// ParseClock validates an `HH:mm` value and returns it as a Clock.
func ParseClock(raw string) (Clock, error) {
	if err := ValidateTimeOfDay(raw); err != nil {
		return "", err
	}

	return Clock(raw), nil
}

// String renders the clock as `HH:mm`.
func (c Clock) String() string {
	return string(c)
}

// Scan implements sql.Scanner. PostgreSQL's text output is `HH:MM:SS[.ffffff]`,
// so the seconds are dropped after validating the value.
func (c *Clock) Scan(src any) error {
	if src == nil {
		*c = ""
		return nil
	}

	var raw string
	switch value := src.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("wiretime: cannot scan %T into Clock", src)
	}

	parts := strings.Split(raw, ":")
	if len(parts) < 2 {
		return fmt.Errorf("wiretime: invalid clock %q", raw)
	}

	candidate := parts[0] + ":" + parts[1]
	if err := ValidateTimeOfDay(candidate); err != nil {
		return fmt.Errorf("wiretime: invalid clock %q", raw)
	}

	*c = Clock(candidate)
	return nil
}

// Value implements driver.Valuer; pgx casts the `HH:mm` text into SQL `time`.
func (c Clock) Value() (driver.Value, error) {
	if c == "" {
		return nil, nil
	}

	if err := ValidateTimeOfDay(string(c)); err != nil {
		return nil, err
	}

	return string(c), nil
}

// ClockText maps an optional Clock to its wire `HH:mm` string.
func ClockText(c *Clock) *string {
	if c == nil {
		return nil
	}

	value := c.String()
	return &value
}

// ClockValue maps an optional wire `HH:mm` string to a Clock. Callers validate
// the input before calling it.
func ClockValue(raw *string) *Clock {
	if raw == nil {
		return nil
	}

	value := Clock(*raw)
	return &value
}
