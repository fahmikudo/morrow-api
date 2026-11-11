package apitest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const importTaskOne = `{
	"id": "t_1", "key": "SB-1", "title": "One", "type": "task", "status": "todo",
	"priority": "medium", "labels": [], "createdAt": "2026-01-01T00:00:00.000Z",
	"updatedAt": "2026-01-01T00:00:00.000Z"
}`

// importBody mirrors the Rust test's `base` helper: an otherwise empty database
// payload with the given version and task list.
func importBody(version int, tasks string) string {
	return fmt.Sprintf(`{
		"version": %d,
		"inbox": [],
		"tasks": [%s],
		"projects": [],
		"notes": [],
		"events": [],
		"areas": [],
		"habits": [],
		"habitCheckIns": [],
		"profile": {
			"id": "me",
			"name": "Raka Pratama",
			"email": "raka@example.com",
			"role": "Product engineer",
			"timezone": "Asia/Jakarta",
			"joinedAt": "2026-01-01T00:00:00.000Z",
			"updatedAt": "2026-01-01T00:00:00.000Z"
		},
		"counters": {}
	}`, version, tasks)
}

func TestImportValidatesAndReplaces(t *testing.T) {
	app := New(t)

	status, _ := app.Post(t, "/api/v1/import", `{ "version": 5 }`)
	if status != http.StatusBadRequest {
		t.Errorf("import version 5 status = %d, want %d", status, http.StatusBadRequest)
	}

	duplicate := importBody(6, importTaskOne+`,

	`+importTaskOne)
	status, body := app.Post(t, "/api/v1/import", duplicate)
	if status != http.StatusBadRequest {
		t.Errorf("duplicate id status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); !strings.Contains(msg, "duplicate id") {
		t.Errorf("duplicate id error = %q, want it to contain %q", msg, "duplicate id")
	}

	unsupported := importBody(5, importTaskOne)
	status, body = app.Post(t, "/api/v1/import", unsupported)
	if status != http.StatusBadRequest {
		t.Errorf("unsupported version status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Invalid import: unsupported version" {
		t.Errorf("unsupported version error = %q, want %q", msg, "Invalid import: unsupported version")
	}

	missingReferenceTask := `{
		"id": "t_1", "key": "SB-1", "title": "One", "type": "task", "status": "todo",
		"priority": "medium", "projectId": "p_missing", "labels": [],
		"createdAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z"
	}`
	status, body = app.Post(t, "/api/v1/import", importBody(6, missingReferenceTask))
	if status != http.StatusBadRequest {
		t.Errorf("missing reference status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); !strings.Contains(msg, "missing reference projectId") {
		t.Errorf("missing reference error = %q, want it to contain %q", msg, "missing reference projectId")
	}

	deep := `{
		"version": 6,
		"inbox": [], "projects": [], "notes": [], "events": [], "areas": [],
		"habits": [], "habitCheckIns": [], "counters": {},
		"profile": {
			"id": "me", "name": "Raka Pratama", "email": "raka@example.com",
			"joinedAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z"
		},
		"tasks": [
			{ "id": "t_1", "key": "SB-1", "title": "Parent", "type": "task", "status": "todo",
			  "priority": "medium", "labels": [], "createdAt": "2026-01-01T00:00:00.000Z",
			  "updatedAt": "2026-01-01T00:00:00.000Z" },
			{ "id": "t_2", "key": "SB-2", "title": "Child", "type": "task", "status": "todo",
			  "priority": "medium", "parentId": "t_1", "labels": [],
			  "createdAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z" },
			{ "id": "t_3", "key": "SB-3", "title": "Grandchild", "type": "task", "status": "todo",
			  "priority": "medium", "parentId": "t_2", "labels": [],
			  "createdAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z" }
		]
	}`
	status, body = app.Post(t, "/api/v1/import", deep)
	if status != http.StatusBadRequest {
		t.Errorf("subtask tree depth status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); !strings.Contains(msg, "subtask tree too deep") {
		t.Errorf("subtask tree error = %q, want it to contain %q", msg, "subtask tree too deep")
	}

	secondWithSameKey := `{
		"id": "t_2", "key": "SB-1", "title": "One", "type": "task", "status": "todo",
		"priority": "medium", "labels": [], "createdAt": "2026-01-01T00:00:00.000Z",
		"updatedAt": "2026-01-01T00:00:00.000Z"
	}`
	duplicateKey := importBody(6, importTaskOne+`,

	`+secondWithSameKey)
	status, body = app.Post(t, "/api/v1/import", duplicateKey)
	if status != http.StatusBadRequest {
		t.Errorf("duplicate task key status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); !strings.Contains(msg, "duplicate task key") {
		t.Errorf("duplicate task key error = %q, want it to contain %q", msg, "duplicate task key")
	}

	payload := `{
		"version": 6,
		"inbox": [{ "id": "i_1", "title": "Idea", "type": "idea",
					"createdAt": "2026-01-01T00:00:00.000Z" }],
		"areas": [{ "id": "a_1", "name": "Work", "tone": "accent", "icon": "briefcase",
					"createdAt": "2026-01-01T00:00:00.000Z",
					"updatedAt": "2026-01-01T00:00:00.000Z" }],
		"projects": [{ "id": "p1", "key": "DWP", "name": "Data Warehouse Platform",
					   "status": "active", "areaId": "a_1",
					   "createdAt": "2026-01-01T00:00:00.000Z",
					   "updatedAt": "2026-01-01T00:00:00.000Z" }],
		"tasks": [
			{ "id": "t_1", "key": "DWP-12", "title": "Ship", "type": "task",
			  "status": "in_progress", "priority": "high", "projectId": "p1",
			  "labels": ["backend"], "createdAt": "2026-01-01T00:00:00.000Z",
			  "updatedAt": "2026-01-01T00:00:00.000Z" },
			{ "id": "t_2", "key": "DWP-13", "title": "Sub", "type": "task",
			  "status": "todo", "priority": "medium", "projectId": "p1",
			  "parentId": "t_1", "labels": [], "createdAt": "2026-01-01T00:00:00.000Z",
			  "updatedAt": "2026-01-01T00:00:00.000Z" }
		],
		"notes": [{ "id": "n_1", "title": "Note", "content": "<p>Body</p>", "tags": ["x"],
					"createdAt": "2026-01-01T00:00:00.000Z",
					"updatedAt": "2026-01-01T00:00:00.000Z" }],
		"events": [{ "id": "e_1", "title": "Sync", "date": "2026-11-08",
					 "startTime": "09:00", "createdAt": "2026-01-01T00:00:00.000Z",
					 "updatedAt": "2026-01-01T00:00:00.000Z" }],
		"habits": [{ "id": "h_1", "title": "Read", "cadence": "daily", "target": 1,
					 "schedule": { "kind": "fixed", "weekdays": [1, 3] },
					 "startDate": "2026-01-01", "createdAt": "2026-01-01T00:00:00.000Z",
					 "updatedAt": "2026-01-01T00:00:00.000Z" }],
		"habitCheckIns": [{ "id": "hc_1", "habitId": "h_1", "date": "2026-01-02",
							"createdAt": "2026-01-02T00:00:00.000Z" }],
		"profile": {
			"id": "me", "name": "Imported Name", "email": "raka@example.com",
			"role": "Engineer", "timezone": "Asia/Jakarta",
			"joinedAt": "2026-01-01T00:00:00.000Z",
			"updatedAt": "2026-01-01T00:00:00.000Z"
		},
		"counters": { "none": 3 }
	}`

	status, body = app.Post(t, "/api/v1/import", payload)
	if status != http.StatusNoContent {
		t.Fatalf("import failed: status = %d, body = %v", status, body)
	}

	status, snapshot := app.Get(t, "/api/v1/snapshot")
	if status != http.StatusOK {
		t.Fatalf("GET /snapshot status = %d, want %d", status, http.StatusOK)
	}
	tasks, ok := snapshot["tasks"].([]any)
	if !ok || len(tasks) != 2 {
		t.Errorf("snapshot tasks = %v, want 2 items", snapshot["tasks"])
	}
	projects, ok := snapshot["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Errorf("snapshot projects = %v, want 1 item", snapshot["projects"])
	}
	habits, ok := snapshot["habits"].([]any)
	if !ok || len(habits) != 1 {
		t.Errorf("snapshot habits = %v, want 1 item", snapshot["habits"])
	}
	checkIns, ok := snapshot["habitCheckIns"].([]any)
	if !ok || len(checkIns) != 1 {
		t.Errorf("snapshot habitCheckIns = %v, want 1 item", snapshot["habitCheckIns"])
	}
	profile, ok := snapshot["profile"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot profile = %v, want object", snapshot["profile"])
	}
	if name, _ := profile["name"].(string); name != "Imported Name" {
		t.Errorf("profile.name = %v, want %q", profile["name"], "Imported Name")
	}
	counters, ok := snapshot["counters"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot counters = %v, want object", snapshot["counters"])
	}
	if got, ok := counters["p1"].(float64); !ok || got != 13 {
		t.Errorf("counters[p1] = %v, want 13", counters["p1"])
	}
	if got, ok := counters["none"].(float64); !ok || got != 3 {
		t.Errorf("counters[none] = %v, want 3", counters["none"])
	}

	_, task := app.Get(t, "/api/v1/tasks/t_2")
	if parentID, _ := task["parentId"].(string); parentID != "t_1" {
		t.Errorf("task t_2 parentId = %v, want %q", task["parentId"], "t_1")
	}

	status, _ = app.GetList(t, "/api/v1/search?q=ship")
	if status != http.StatusOK {
		t.Errorf("GET /search?q=ship status = %d, want %d", status, http.StatusOK)
	}
}

func TestImportRejectsDuplicateCheckIns(t *testing.T) {
	app := New(t)

	payload := `{
		"version": 6,
		"inbox": [], "tasks": [], "projects": [], "notes": [], "events": [], "areas": [],
		"habits": [{ "id": "h_1", "title": "Read", "cadence": "daily", "target": 1,
					 "schedule": { "kind": "flexible" }, "startDate": "2026-01-01",
					 "createdAt": "2026-01-01T00:00:00.000Z",
					 "updatedAt": "2026-01-01T00:00:00.000Z" }],
		"habitCheckIns": [
			{ "id": "hc_1", "habitId": "h_1", "date": "2026-01-02",
			  "createdAt": "2026-01-02T00:00:00.000Z" },
			{ "id": "hc_2", "habitId": "h_1", "date": "2026-01-02",
			  "createdAt": "2026-01-02T01:00:00.000Z" }
		],
		"profile": {
			"id": "me", "name": "Raka Pratama", "email": "raka@example.com",
			"joinedAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z"
		},
		"counters": {}
	}`

	status, body := app.Post(t, "/api/v1/import", payload)
	if status != http.StatusBadRequest {
		t.Errorf("duplicate check-in status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Invalid import: duplicate check-in for h_1 on 2026-01-02" {
		t.Errorf("duplicate check-in error = %q, want %q", msg, "Invalid import: duplicate check-in for h_1 on 2026-01-02")
	}
}

func TestImportRejectsEndDateBeforeStartDate(t *testing.T) {
	app := New(t)

	payload := `{
		"version": 6,
		"inbox": [], "tasks": [], "projects": [], "notes": [], "events": [], "areas": [],
		"habits": [{ "id": "h_1", "title": "Read", "cadence": "daily", "target": 1,
					 "schedule": { "kind": "flexible" }, "startDate": "2026-01-02",
					 "endDate": "2026-01-01",
					 "createdAt": "2026-01-01T00:00:00.000Z",
					 "updatedAt": "2026-01-01T00:00:00.000Z" }],
		"habitCheckIns": [],
		"profile": {
			"id": "me", "name": "Raka Pratama", "email": "raka@example.com",
			"joinedAt": "2026-01-01T00:00:00.000Z", "updatedAt": "2026-01-01T00:00:00.000Z"
		},
		"counters": {}
	}`

	status, body := app.Post(t, "/api/v1/import", payload)
	if status != http.StatusBadRequest {
		t.Errorf("end before start status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Invalid import: end date before start date for h_1" {
		t.Errorf("end before start error = %q", msg)
	}
}
