package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestProjectKeyDerivationAndCollisions(t *testing.T) {
	app := New(t)

	cases := []struct{ name, expected string }{
		{"Data Warehouse Platform", "DWP"},
		{"Design", "DES"},
		{"Go", "GO"},
		{"A", "AX"},
		{"123", "SB"},
	}

	for _, tc := range cases {
		status, project := app.Post(t, "/api/v1/projects", fmt.Sprintf(`{"name":%q}`, tc.name))
		if status != http.StatusCreated {
			t.Errorf("create project %q: status %d, want %d", tc.name, status, http.StatusCreated)
		}
		if got := StringField(t, project, "key"); got != tc.expected {
			t.Errorf("project key for %q = %q, want %q", tc.name, got, tc.expected)
		}
	}

	_, duplicate := app.Post(t, "/api/v1/projects", `{"name":"Design"}`)
	if got := StringField(t, duplicate, "key"); got != "DES2" {
		t.Errorf("duplicate project key = %q, want %q", got, "DES2")
	}
}

func TestProjectDeleteClearsReferences(t *testing.T) {
	app := New(t)

	_, project := app.Post(t, "/api/v1/projects", `{"name":"Doomed Project"}`)
	projectID := StringField(t, project, "id")

	_, task := app.Post(t, "/api/v1/tasks",
		fmt.Sprintf(`{"title":"Task","projectId":%q}`, projectID))
	_, note := app.Post(t, "/api/v1/notes",
		fmt.Sprintf(`{"title":"Note","projectId":%q}`, projectID))
	_, event := app.Post(t, "/api/v1/events",
		fmt.Sprintf(`{"title":"Event","date":"2026-11-10","projectId":%q}`, projectID))
	_, habit := app.Post(t, "/api/v1/habits",
		fmt.Sprintf(`{"title":"Habit","projectId":%q,"startDate":"2026-11-01"}`, projectID))

	taskID := StringField(t, task, "id")
	noteID := StringField(t, note, "id")
	eventID := StringField(t, event, "id")
	habitID := StringField(t, habit, "id")

	if status := app.Delete(t, "/api/v1/projects/"+projectID); status != http.StatusNoContent {
		t.Fatalf("delete project: status %d, want %d", status, http.StatusNoContent)
	}

	checks := []struct{ path, id string }{
		{"/api/v1/tasks", taskID},
		{"/api/v1/notes", noteID},
		{"/api/v1/events", eventID},
		{"/api/v1/habits", habitID},
	}

	for _, check := range checks {
		path := check.path + "/" + check.id
		status, entity := app.Get(t, path)
		if status != http.StatusOK {
			t.Errorf("path %s: status %d, want %d", path, status, http.StatusOK)
		}
		if _, present := entity["projectId"]; present {
			t.Errorf("%s still references the project", path)
		}
	}
}
