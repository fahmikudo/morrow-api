package apitest

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"morrow-api/internal/platform/wiretime"
)

var wireTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

func TestWireTimestampFormat(t *testing.T) {
	encoded, err := json.Marshal(wiretime.Now())
	if err != nil {
		t.Fatalf("marshal timestamp: %v", err)
	}

	var rendered string
	if err := json.Unmarshal(encoded, &rendered); err != nil {
		t.Fatalf("unmarshal timestamp: %v", err)
	}

	if !wireTimestamp.MatchString(rendered) {
		t.Fatalf("timestamp %q is not millisecond RFC 3339 with Z", rendered)
	}

	expectedLength := len("2026-11-08T03:15:27.412Z")
	if len(rendered) != expectedLength {
		t.Fatalf("timestamp %q has length %d, want %d", rendered, len(rendered), expectedLength)
	}
}

func TestWireTimestampParsesAnyFraction(t *testing.T) {
	var parsed wiretime.Timestamp
	if err := json.Unmarshal([]byte(`"2026-11-08T03:15:27.4Z"`), &parsed); err != nil {
		t.Fatalf("parse short fraction: %v", err)
	}

	if !parsed.Time().Equal(time.Date(2026, 11, 8, 3, 15, 27, 400_000_000, time.UTC)) {
		t.Fatalf("parsed instant %v", parsed.Time())
	}
}

func TestWireDateIsStrict(t *testing.T) {
	var parsed wiretime.Date
	if err := json.Unmarshal([]byte(`"2026-11-10"`), &parsed); err != nil {
		t.Fatalf("parse date: %v", err)
	}
	if actual := parsed.String(); actual != "2026-11-10" {
		t.Fatalf("date round trip = %q", actual)
	}

	if err := json.Unmarshal([]byte(`"2026-1-2"`), &parsed); err == nil {
		t.Fatalf("unpadded date was accepted")
	}
}
