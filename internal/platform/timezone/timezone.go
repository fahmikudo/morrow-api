// Package timezone resolves user-facing local time while backend timestamps stay
// in UTC. The profile timezone wins; APP_TIMEZONE is the deployment fallback.
package timezone

import (
	"strings"
	"time"
)

// Resolve returns the profile timezone when valid, then the configured
// fallback, then UTC.
func Resolve(profileTimezone *string, fallback string) *time.Location {
	if location := load(profileTimezone); location != nil {
		return location
	}
	if location := load(&fallback); location != nil {
		return location
	}

	return time.UTC
}

func load(name *string) *time.Location {
	if name == nil {
		return nil
	}
	name = stringPtr(strings.TrimSpace(*name))
	if name == nil {
		return nil
	}

	location, err := time.LoadLocation(*name)
	if err != nil {
		return nil
	}

	return location
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}
