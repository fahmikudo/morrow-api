package wiretime

import (
	"errors"
	"regexp"
)

// ErrInvalidTimeOfDay is returned by ValidateTimeOfDay.
var ErrInvalidTimeOfDay = errors.New("invalid HH:mm value")

var timeOfDay = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidateTimeOfDay enforces the 24-hour `HH:mm` shape used by events and habits.
func ValidateTimeOfDay(value string) error {
	if !timeOfDay.MatchString(value) {
		return ErrInvalidTimeOfDay
	}
	return nil
}
