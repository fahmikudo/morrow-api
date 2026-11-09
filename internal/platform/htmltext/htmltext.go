// Package htmltext converts stored rich text into the plain text the search,
// assistant, and habit labels show.
package htmltext

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	blockTag   = regexp.MustCompile(`(?i)<(p|div|li|h[1-6]|blockquote|pre|br|table|tr|td|th)\b[^>]*>`)
	anyTag     = regexp.MustCompile(`<[^>]+>`)
	whitespace = regexp.MustCompile(`[\s\p{Z}]+`)
)

// StripHTML turns the app's HTML into a single-space-collapsed string.
func StripHTML(html string) string {
	if html == "" {
		return ""
	}

	spaced := blockTag.ReplaceAllString(html, " ")
	stripped := anyTag.ReplaceAllString(spaced, "")
	entities := strings.ReplaceAll(stripped, "&nbsp;", " ")

	return strings.TrimSpace(whitespace.ReplaceAllString(entities, " "))
}

// Truncate keeps at most max runes and appends an ellipsis when it cut anything.
func Truncate(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}

	return strings.TrimRight(string(runes[:max]), " ") + "…"
}

// Excerpt strips HTML first, then truncates.
func Excerpt(html string, max int) string {
	return Truncate(StripHTML(html), max)
}

// HabitCadenceLabel renders the subtitle fragment search and the assistant use.
func HabitCadenceLabel(cadence string, target int32) string {
	if target < 1 {
		target = 1
	}

	count := strconv.FormatInt(int64(target), 10)
	switch cadence {
	case "daily":
		if target == 1 {
			return "Daily"
		}
		return count + "×/day"
	case "weekly":
		return count + "×/week"
	case "monthly":
		return count + "×/month"
	default:
		return count + "×/" + cadence
	}
}
