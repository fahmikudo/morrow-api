package apitest

import (
	"testing"

	"morrow-api/internal/platform/keygen"
)

func TestDeriveKeyPrefix(t *testing.T) {
	cases := []struct {
		name     string
		expected string
	}{
		{"Data Warehouse Platform", "DWP"},
		{"Design", "DES"},
		{"Go", "GO"},
		{"A", "AX"},
		{"123", "SB"},
		{"", "SB"},
		{"Work Project", "WP"},
		{"Concurrency", "CON"},
	}

	for _, testCase := range cases {
		if actual := keygen.DeriveKeyPrefix(testCase.name); actual != testCase.expected {
			t.Errorf("DeriveKeyPrefix(%q) = %q, want %q", testCase.name, actual, testCase.expected)
		}
	}
}

func TestUniqueKey(t *testing.T) {
	taken := map[string]struct{}{"DES": {}, "DES2": {}}

	if actual := keygen.UniqueKey("DES", taken); actual != "DES3" {
		t.Errorf("UniqueKey(\"DES\") = %q, want \"DES3\"", actual)
	}
	if actual := keygen.UniqueKey("DWP", taken); actual != "DWP" {
		t.Errorf("UniqueKey(\"DWP\") = %q, want \"DWP\"", actual)
	}
}
