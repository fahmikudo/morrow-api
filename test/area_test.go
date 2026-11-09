package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAreaDeleteClearsReferences(t *testing.T) {
	app := New(t)

	_, area := app.Post(t, "/api/v1/areas", `{"name":"Work"}`)
	areaID := StringField(t, area, "id")
	if got := StringField(t, area, "tone"); got != "neutral" {
		t.Errorf("area tone = %q, want %q", got, "neutral")
	}
	if got := StringField(t, area, "icon"); got != "sparkles" {
		t.Errorf("area icon = %q, want %q", got, "sparkles")
	}

	_, project := app.Post(t, "/api/v1/projects",
		fmt.Sprintf(`{"name":"Work Project","areaId":%q}`, areaID))
	projectID := StringField(t, project, "id")

	_, task := app.Post(t, "/api/v1/tasks",
		fmt.Sprintf(`{"title":"Task","areaId":%q}`, areaID))
	taskID := StringField(t, task, "id")

	_, note := app.Post(t, "/api/v1/notes",
		fmt.Sprintf(`{"title":"Note","areaId":%q}`, areaID))
	noteID := StringField(t, note, "id")

	_, habit := app.Post(t, "/api/v1/habits",
		fmt.Sprintf(`{"title":"Habit","areaId":%q}`, areaID))
	habitID := StringField(t, habit, "id")

	if status := app.Delete(t, "/api/v1/areas/"+areaID); status != http.StatusNoContent {
		t.Fatalf("delete area: status %d, want %d", status, http.StatusNoContent)
	}

	_, project = app.Get(t, "/api/v1/projects/"+projectID)
	if _, present := project["areaId"]; present {
		t.Errorf("project still references the area")
	}

	_, task = app.Get(t, "/api/v1/tasks/"+taskID)
	if _, present := task["areaId"]; present {
		t.Errorf("task still references the area")
	}

	_, note = app.Get(t, "/api/v1/notes/"+noteID)
	if _, present := note["areaId"]; present {
		t.Errorf("note still references the area")
	}

	_, habit = app.Get(t, "/api/v1/habits/"+habitID)
	if _, present := habit["areaId"]; present {
		t.Errorf("habit still references the area")
	}
}
