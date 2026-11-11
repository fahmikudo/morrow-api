package apitest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/search"
	"morrow-api/internal/module/snapshot"
	"morrow-api/internal/module/task"
)

// roundTrip proves the wire structs read and write the shared fixtures without
// renaming, dropping, or inventing a field.
func roundTrip[T any](t *testing.T, fixture string) {
	t.Helper()

	path := filepath.Join("..", "contracts", "fixtures", fixture)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var expected any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("decode fixture %s: %v", fixture, err)
	}

	var parsed T
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("deserialize %s: %v", fixture, err)
	}

	encoded, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("serialize %s: %v", fixture, err)
	}

	var actual any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatalf("decode reserialized %s: %v", fixture, err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("round trip mismatch for %s\nexpected: %s\nactual:   %s", fixture, raw, encoded)
	}
}

func TestTaskFixtureRoundTrips(t *testing.T) {
	roundTrip[task.Task](t, "task.json")
}

func TestMinimalTaskFixtureRoundTrips(t *testing.T) {
	roundTrip[task.Task](t, "task-minimal.json")
}

func TestProjectFixtureRoundTrips(t *testing.T) {
	roundTrip[project.Project](t, "project.json")
}

func TestNoteFixtureRoundTrips(t *testing.T) {
	roundTrip[note.Note](t, "note.json")
}

func TestEventFixtureRoundTrips(t *testing.T) {
	roundTrip[event.Event](t, "event.json")
}

func TestAreaFixtureRoundTrips(t *testing.T) {
	roundTrip[area.Area](t, "area.json")
}

func TestHabitFixtureRoundTrips(t *testing.T) {
	roundTrip[habit.Habit](t, "habit.json")
}

func TestHabitCheckInFixtureRoundTrips(t *testing.T) {
	roundTrip[habit.CheckIn](t, "habit-check-in.json")
}

func TestInboxItemFixtureRoundTrips(t *testing.T) {
	roundTrip[inbox.Item](t, "inbox-item.json")
}

func TestProfileFixtureRoundTrips(t *testing.T) {
	roundTrip[profile.Profile](t, "profile.json")
}

func TestSearchResultsFixtureRoundTrips(t *testing.T) {
	roundTrip[[]search.Result](t, "search-results.json")
}

func TestSnapshotFixtureRoundTrips(t *testing.T) {
	roundTrip[snapshot.Response](t, "server-snapshot.json")
}
