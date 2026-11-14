package apitest

import (
	"net/http"
	"strings"
	"testing"
)

func TestSnapshotShapeMatchesTheContract(t *testing.T) {
	app := New(t)

	status, snapshot := app.Get(t, "/api/v1/snapshot")
	if status != http.StatusOK {
		t.Fatalf("GET /snapshot status = %d, want %d", status, http.StatusOK)
	}

	for _, key := range []string{
		"version",
		"inbox",
		"tasks",
		"projects",
		"notes",
		"events",
		"areas",
		"habits",
		"habitCheckIns",
		"habitSkips",
		"profile",
		"counters",
		"integrations",
		"ai",
		"serverTime",
	} {
		if _, ok := snapshot[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}

	if version, ok := snapshot["version"].(float64); !ok || version != 6 {
		t.Errorf("version = %v, want 6", snapshot["version"])
	}

	profile, ok := snapshot["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile = %v, want object", snapshot["profile"])
	}
	if id, ok := profile["id"].(string); !ok || id != "me" {
		t.Errorf("profile.id = %v, want %q", profile["id"], "me")
	}

	integrations, ok := snapshot["integrations"].(map[string]any)
	if !ok {
		t.Fatalf("integrations = %v, want object", snapshot["integrations"])
	}
	if len(integrations) != 2 {
		t.Errorf("integrations length = %d, want 2", len(integrations))
	}

	serverTime, ok := snapshot["serverTime"].(string)
	if !ok {
		t.Fatalf("serverTime = %v, want string", snapshot["serverTime"])
	}
	if !strings.HasSuffix(serverTime, "Z") {
		t.Errorf("serverTime %s", serverTime)
	}
	if len(serverTime) != len("2026-11-08T03:15:27.412Z") {
		t.Errorf("serverTime length = %d, want %d (millisecond precision)", len(serverTime), len("2026-11-08T03:15:27.412Z"))
	}
}
