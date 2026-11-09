package task

import (
	"strings"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// CleanLabels trims each label, drops the empty ones, and keeps order and
// duplicates.
func CleanLabels(labels []string) []string {
	cleaned := make([]string, 0, len(labels))
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return cleaned
}

// validateTimeBox enforces optional `HH:mm` bounds and their order.
func validateTimeBox(startTime, endTime *string) error {
	if startTime != nil {
		if err := wiretime.ValidateTimeOfDay(*startTime); err != nil {
			return httpx.BadRequest("Use an HH:mm time")
		}
	}

	if endTime != nil {
		if err := wiretime.ValidateTimeOfDay(*endTime); err != nil {
			return httpx.BadRequest("Use an HH:mm time")
		}
	}

	if startTime != nil && endTime != nil && *endTime <= *startTime {
		return httpx.BadRequest("End time must be after start time")
	}

	return nil
}
