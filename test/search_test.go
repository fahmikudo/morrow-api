package apitest

import (
	"fmt"
	"testing"
)

func searchResultByKind(t *testing.T, results []any, kind string) map[string]any {
	t.Helper()
	for _, entry := range results {
		item, ok := entry.(map[string]any)
		if ok && item["kind"] == kind {
			return item
		}
	}
	t.Fatalf("no %q result in %v", kind, results)
	return nil
}

func TestSearchLimitsAndSubtitles(t *testing.T) {
	app := New(t)

	status, project := app.Post(t, "/api/v1/projects", `{"name": "Warehouse"}`)
	if status != 201 {
		t.Fatalf("create project status = %d, want 201", status)
	}
	projectID := StringField(t, project, "id")

	for index := range 6 {
		_, task := app.Post(t, "/api/v1/tasks", fmt.Sprintf(`{"title": "Warehouse task %d", "projectId": %q}`, index, projectID))
		if StringField(t, task, "id") == "" {
			t.Fatalf("task %d has no id: %v", index, task)
		}
	}

	_, note := app.Post(t, "/api/v1/notes", `{"title": "Warehouse note", "content": "<p>Nightly warehouse sync</p>"}`)
	noteID := StringField(t, note, "id")

	status, results := app.GetList(t, "/api/v1/search?q=warehouse")
	if status != 200 {
		t.Fatalf("search status = %d, want 200", status)
	}
	if len(results) == 0 {
		t.Fatalf("search returned no results")
	}

	kinds := make([]string, 0, len(results))
	taskCount := 0
	for _, entry := range results {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("search result is not an object: %v", entry)
		}
		kind, _ := item["kind"].(string)
		kinds = append(kinds, kind)
		if kind == "task" {
			taskCount++
		}
	}

	if kinds[0] != "task" {
		t.Errorf("first kind = %q, want task", kinds[0])
	}
	if taskCount != 5 {
		t.Errorf("task result count = %d, want 5", taskCount)
	}
	hasNote := false
	hasProject := false
	for _, kind := range kinds {
		if kind == "note" {
			hasNote = true
		}
		if kind == "project" {
			hasProject = true
		}
	}
	if !hasNote {
		t.Errorf("results %v contain no note result", kinds)
	}
	if !hasProject {
		t.Errorf("results %v contain no project result", kinds)
	}

	task := results[0].(map[string]any)
	if got := task["href"]; got != "/tasks" {
		t.Errorf("task href = %v, want /tasks", got)
	}
	if got := task["subtitle"]; got != "WAR-1 · Warehouse" {
		t.Errorf("task subtitle = %v, want %q", got, "WAR-1 · Warehouse")
	}

	noteResult := searchResultByKind(t, results, "note")
	if got, want := noteResult["href"], "/knowledge/"+noteID; got != want {
		t.Errorf("note href = %v, want %v", got, want)
	}

	status, empty := app.GetList(t, "/api/v1/search?q=%20")
	if status != 200 {
		t.Fatalf("blank search status = %d, want 200", status)
	}
	if len(empty) != 0 {
		t.Errorf("blank search results = %v, want empty", empty)
	}
}

func TestSearchCoversEveryKindInContractOrder(t *testing.T) {
	app := New(t)

	status, area := app.Post(t, "/api/v1/areas", `{"name": "Warehouse area", "description": "Warehouse operations"}`)
	if status != 201 {
		t.Fatalf("create area status = %d, want 201", status)
	}
	areaID := StringField(t, area, "id")

	status, project := app.Post(t, "/api/v1/projects", fmt.Sprintf(`{"name": "Warehouse project", "areaId": %q}`, areaID))
	if status != 201 {
		t.Fatalf("create project status = %d, want 201", status)
	}
	projectID := StringField(t, project, "id")

	app.Post(t, "/api/v1/tasks", fmt.Sprintf(`{"title": "Warehouse task", "projectId": %q}`, projectID))
	app.Post(t, "/api/v1/notes", `{"title": "Warehouse note", "content": "<p>Warehouse body</p>"}`)
	app.Post(t, "/api/v1/inbox", `{"title": "Warehouse capture", "type": "note"}`)
	status, habit := app.Post(t, "/api/v1/habits", `{"title": "Warehouse habit", "cadence": "weekly", "target": 2, "startDate": "2026-11-01"}`)
	if status != 201 {
		t.Fatalf("create habit status = %d, want 201", status)
	}
	habitID := StringField(t, habit, "id")

	_, results := app.GetList(t, "/api/v1/search?q=warehouse")
	if len(results) == 0 {
		t.Fatalf("search returned no results")
	}

	kinds := make([]string, 0, len(results))
	for _, entry := range results {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("search result is not an object: %v", entry)
		}
		kind, _ := item["kind"].(string)
		kinds = append(kinds, kind)
	}
	want := []string{"task", "note", "project", "inbox", "habit", "area"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}

	habitResult := searchResultByKind(t, results, "habit")
	if got := habitResult["id"]; got != habitID {
		t.Errorf("habit id = %v, want %v", got, habitID)
	}
	if got := habitResult["subtitle"]; got != "2×/week" {
		t.Errorf("habit subtitle = %v, want %q", got, "2×/week")
	}
	if got := habitResult["href"]; got != "/habits" {
		t.Errorf("habit href = %v, want /habits", got)
	}

	areaResult := searchResultByKind(t, results, "area")
	if got := areaResult["subtitle"]; got != "Warehouse operations" {
		t.Errorf("area subtitle = %v, want %q", got, "Warehouse operations")
	}
	if got, wantHref := areaResult["href"], "/areas/"+areaID; got != wantHref {
		t.Errorf("area href = %v, want %v", got, wantHref)
	}

	projectResult := searchResultByKind(t, results, "project")
	if got := projectResult["subtitle"]; got != "WP · Project" {
		t.Errorf("project subtitle = %v, want %q", got, "WP · Project")
	}
}
