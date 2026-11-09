package apitest

import (
	"testing"

	"morrow-api/internal/platform/htmltext"
)

func TestStripHTML(t *testing.T) {
	cases := []struct {
		html     string
		expected string
	}{
		{"", ""},
		{"<p>Nightly sync runs at 02:00</p>", "Nightly sync runs at 02:00"},
		{"<p>One</p><p>Two</p>", "One Two"},
		{"line<br/>break", "line break"},
		{"a&nbsp;b", "a b"},
		{"  <div>  spaced   out  </div>  ", "spaced out"},
		{"plain text", "plain text"},
		{"<h2>Title</h2><ul><li>a</li><li>b</li></ul>", "Title a b"},
	}

	for _, testCase := range cases {
		if actual := htmltext.StripHTML(testCase.html); actual != testCase.expected {
			t.Errorf("StripHTML(%q) = %q, want %q", testCase.html, actual, testCase.expected)
		}
	}
}

func TestTruncateAndExcerpt(t *testing.T) {
	if actual := htmltext.Truncate("short", 10); actual != "short" {
		t.Errorf("Truncate kept %q", actual)
	}
	if actual := htmltext.Truncate("0123456789", 5); actual != "01234…" {
		t.Errorf("Truncate = %q, want %q", actual, "01234…")
	}
	if actual := htmltext.Excerpt("<p>Nightly sync runs at 02:00</p>", 7); actual != "Nightly…" {
		t.Errorf("Excerpt = %q, want %q", actual, "Nightly…")
	}
}

func TestHabitCadenceLabel(t *testing.T) {
	cases := []struct {
		cadence  string
		target   int32
		expected string
	}{
		{"daily", 1, "Daily"},
		{"daily", 3, "3×/day"},
		{"weekly", 1, "1×/week"},
		{"weekly", 4, "4×/week"},
		{"monthly", 2, "2×/month"},
		{"daily", 0, "Daily"},
	}

	for _, testCase := range cases {
		actual := htmltext.HabitCadenceLabel(testCase.cadence, testCase.target)
		if actual != testCase.expected {
			t.Errorf("HabitCadenceLabel(%q, %d) = %q, want %q",
				testCase.cadence, testCase.target, actual, testCase.expected)
		}
	}
}
