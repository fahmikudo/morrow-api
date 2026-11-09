package apitest

import (
	"net/http"
	"sort"
	"sync"
	"testing"
)

// taskAssertAbsent checks that a key is missing from a decoded JSON object.
func taskAssertAbsent(t *testing.T, body map[string]any, key, detail string) {
	t.Helper()
	if _, ok := body[key]; ok {
		t.Errorf("%s: %s should be absent, got %v", detail, key, body[key])
	}
}

// TestTaskKeysIncrementPerProject ports task_keys_increment_per_project.
func TestTaskKeysIncrementPerProject(t *testing.T) {
	app := New(t)

	status, project := app.Post(t, "/api/v1/projects", `{"name":"Warehouse Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create project status = %d, want %d", status, http.StatusCreated)
	}
	if got := StringField(t, project, "key"); got != "WP" {
		t.Errorf("project key = %q, want %q", got, "WP")
	}
	projectID := StringField(t, project, "id")

	status, first := app.Post(t, "/api/v1/tasks", `{"title":"Sync job","projectId":"`+projectID+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("create first task status = %d, want %d", status, http.StatusCreated)
	}
	if got := StringField(t, first, "key"); got != "WP-1" {
		t.Errorf("first key = %q, want %q", got, "WP-1")
	}

	_, second := app.Post(t, "/api/v1/tasks", `{"title":"Second","projectId":"`+projectID+`"}`)
	if got := StringField(t, second, "key"); got != "WP-2" {
		t.Errorf("second key = %q, want %q", got, "WP-2")
	}

	_, loose := app.Post(t, "/api/v1/tasks", `{"title":"Loose end"}`)
	if got := StringField(t, loose, "key"); got != "SB-1" {
		t.Errorf("loose key = %q, want %q", got, "SB-1")
	}

	status, snapshot := app.Get(t, "/api/v1/snapshot")
	if status != http.StatusOK {
		t.Fatalf("snapshot status = %d, want %d", status, http.StatusOK)
	}
	counters, ok := snapshot["counters"].(map[string]any)
	if !ok {
		t.Fatalf("counters is %T, want object", snapshot["counters"])
	}
	if got, ok := counters["none"].(float64); !ok || got != 1 {
		t.Errorf("counters[\"none\"] = %v, want 1", counters["none"])
	}
	if got, ok := counters[projectID].(float64); !ok || got != 2 {
		t.Errorf("counters[%q] = %v, want 2", projectID, counters[projectID])
	}
}

// TestTaskTimeBox covers the optional start/end time fields.
func TestTaskTimeBox(t *testing.T) {
	app := New(t)

	status, created := app.Post(
		t, "/api/v1/tasks",
		`{"title":"Deep work","startTime":"09:00","endTime":"10:30"}`,
	)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%v)", status, created)
	}
	if got := StringField(t, created, "startTime"); got != "09:00" {
		t.Errorf("startTime = %q, want 09:00", got)
	}
	if got := StringField(t, created, "endTime"); got != "10:30" {
		t.Errorf("endTime = %q, want 10:30", got)
	}
	taskID := StringField(t, created, "id")

	status, cleared := app.Patch(
		t, "/api/v1/tasks/"+taskID,
		`{"startTime":null,"endTime":null}`,
	)
	if status != http.StatusOK {
		t.Fatalf("clear status = %d, want 200 (%v)", status, cleared)
	}
	taskAssertAbsent(t, cleared, "startTime", "clear")
	taskAssertAbsent(t, cleared, "endTime", "clear")

	status, body := app.Patch(t, "/api/v1/tasks/"+taskID, `{"startTime":"9:00"}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "Use an HH:mm time" {
		t.Errorf("bad start = %d %q, want 400 Use an HH:mm time", status, ErrorMessage(body))
	}

	status, body = app.Patch(t, "/api/v1/tasks/"+taskID, `{"startTime":"10:00","endTime":"10:00"}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "End time must be after start time" {
		t.Errorf("equal ends = %d %q, want 400 End time must be after start time", status, ErrorMessage(body))
	}

	status, endOnly := app.Patch(t, "/api/v1/tasks/"+taskID, `{"endTime":"11:00"}`)
	if status != http.StatusOK {
		t.Fatalf("end only status = %d, want 200 (%v)", status, endOnly)
	}
	if got := StringField(t, endOnly, "endTime"); got != "11:00" {
		t.Errorf("endTime = %q, want 11:00", got)
	}
	taskAssertAbsent(t, endOnly, "startTime", "end only")

	status, body = app.Patch(t, "/api/v1/tasks/"+taskID, `{"startTime":"12:00"}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "End time must be after start time" {
		t.Errorf("start after end = %d %q, want 400 End time must be after start time", status, ErrorMessage(body))
	}
}

// TestConcurrentTaskCreatesGetDistinctKeys ports
// concurrent_task_creates_get_distinct_keys.
func TestConcurrentTaskCreatesGetDistinctKeys(t *testing.T) {
	app := New(t)

	_, project := app.Post(t, "/api/v1/projects", `{"name":"Concurrency"}`)
	projectID := StringField(t, project, "id")

	body := `{"title":"A","projectId":"` + projectID + `"}`
	bodyB := `{"title":"B","projectId":"` + projectID + `"}`

	var wg sync.WaitGroup
	var (
		statusA, statusB int
		resultA, resultB any
		errA, errB       error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		statusA, resultA, errA = app.Call(http.MethodPost, "/api/v1/tasks", app.Token, body)
	}()
	go func() {
		defer wg.Done()
		statusB, resultB, errB = app.Call(http.MethodPost, "/api/v1/tasks", app.Token, bodyB)
	}()
	wg.Wait()

	if errA != nil {
		t.Fatalf("create A: %v", errA)
	}
	if errB != nil {
		t.Fatalf("create B: %v", errB)
	}
	if statusA != http.StatusCreated {
		t.Errorf("create A status = %d, want %d", statusA, http.StatusCreated)
	}
	if statusB != http.StatusCreated {
		t.Errorf("create B status = %d, want %d", statusB, http.StatusCreated)
	}

	first, ok := resultA.(map[string]any)
	if !ok {
		t.Fatalf("create A body is %T, want object", resultA)
	}
	second, ok := resultB.(map[string]any)
	if !ok {
		t.Fatalf("create B body is %T, want object", resultB)
	}

	keys := []string{
		StringField(t, first, "key"),
		StringField(t, second, "key"),
	}
	sort.Strings(keys)
	want := []string{"CON-1", "CON-2"}
	if len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

// TestSubtasksInheritProjectAndValidateParents ports
// subtasks_inherit_project_and_validate_parents.
func TestSubtasksInheritProjectAndValidateParents(t *testing.T) {
	app := New(t)

	_, project := app.Post(t, "/api/v1/projects", `{"name":"Platform Run"}`)
	projectID := StringField(t, project, "id")

	_, parent := app.Post(t, "/api/v1/tasks", `{"title":"Parent","projectId":"`+projectID+`"}`)
	parentID := StringField(t, parent, "id")

	status, child := app.Post(t, "/api/v1/tasks", `{"title":"Child","parentId":"`+parentID+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("create child status = %d, want %d", status, http.StatusCreated)
	}
	if got := StringField(t, child, "projectId"); got != projectID {
		t.Errorf("child projectId = %q, want %q", got, projectID)
	}
	if got := StringField(t, child, "parentId"); got != parentID {
		t.Errorf("child parentId = %q, want %q", got, parentID)
	}
	childID := StringField(t, child, "id")

	status, body := app.Post(t, "/api/v1/tasks", `{"title":"Grandchild","parentId":"`+childID+`"}`)
	if status != http.StatusBadRequest {
		t.Errorf("grandchild status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Subtasks cannot have subtasks" {
		t.Errorf("grandchild error = %q, want %q", msg, "Subtasks cannot have subtasks")
	}

	status, body = app.Post(t, "/api/v1/tasks", `{"title":"Orphan","parentId":"t_missing"}`)
	if status != http.StatusBadRequest {
		t.Errorf("orphan status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Parent task not found" {
		t.Errorf("orphan error = %q, want %q", msg, "Parent task not found")
	}

	status, body = app.Patch(t, "/api/v1/tasks/"+parentID, `{"parentId":"`+parentID+`"}`)
	if status != http.StatusBadRequest {
		t.Errorf("self-parent status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "A task cannot be its own parent" {
		t.Errorf("self-parent error = %q, want %q", msg, "A task cannot be its own parent")
	}
}

// TestReparentMovesProjectAndChildrenBlock ports
// reparent_moves_project_and_children_block.
func TestReparentMovesProjectAndChildrenBlock(t *testing.T) {
	app := New(t)

	_, projectA := app.Post(t, "/api/v1/projects", `{"name":"Alpha"}`)
	_, projectB := app.Post(t, "/api/v1/projects", `{"name":"Beta"}`)
	alpha := StringField(t, projectA, "id")
	beta := StringField(t, projectB, "id")

	_, parent := app.Post(t, "/api/v1/tasks", `{"title":"Parent","projectId":"`+beta+`"}`)
	parentID := StringField(t, parent, "id")

	_, task := app.Post(t, "/api/v1/tasks", `{"title":"Task","projectId":"`+alpha+`"}`)
	taskID := StringField(t, task, "id")

	status, moved := app.Patch(t, "/api/v1/tasks/"+taskID, `{"parentId":"`+parentID+`"}`)
	if status != http.StatusOK {
		t.Fatalf("reparent status = %d, want %d", status, http.StatusOK)
	}
	if got := StringField(t, moved, "parentId"); got != parentID {
		t.Errorf("moved parentId = %q, want %q", got, parentID)
	}
	if got := StringField(t, moved, "projectId"); got != beta {
		t.Errorf("moved projectId = %q, want %q", got, beta)
	}

	_, child := app.Post(t, "/api/v1/tasks", `{"title":"Child","parentId":"`+parentID+`"}`)
	childID := StringField(t, child, "id")

	status, body := app.Patch(t, "/api/v1/tasks/"+parentID, `{"parentId":"`+taskID+`"}`)
	if status != http.StatusBadRequest {
		t.Errorf("children-block status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "A task with subtasks cannot become a subtask" {
		t.Errorf("children-block error = %q, want %q", msg, "A task with subtasks cannot become a subtask")
	}

	if status := app.Delete(t, "/api/v1/tasks/"+parentID); status != http.StatusNoContent {
		t.Errorf("delete parent status = %d, want %d", status, http.StatusNoContent)
	}

	status, detached := app.Get(t, "/api/v1/tasks/"+childID)
	if status != http.StatusOK {
		t.Fatalf("get child status = %d, want %d", status, http.StatusOK)
	}
	taskAssertAbsent(t, detached, "parentId", "child should detach")
}

// TestNullClearsOptionalTaskFields ports null_clears_optional_task_fields.
func TestNullClearsOptionalTaskFields(t *testing.T) {
	app := New(t)

	_, project := app.Post(t, "/api/v1/projects", `{"name":"Clearing"}`)
	projectID := StringField(t, project, "id")

	_, task := app.Post(t, "/api/v1/tasks", `{"title":"Has fields","projectId":"`+projectID+`","dueDate":"2026-11-10"}`)
	taskID := StringField(t, task, "id")

	status, cleared := app.Patch(t, "/api/v1/tasks/"+taskID, `{"dueDate":null,"projectId":null}`)
	if status != http.StatusOK {
		t.Fatalf("clear status = %d, want %d", status, http.StatusOK)
	}
	taskAssertAbsent(t, cleared, "dueDate", "cleared task")
	taskAssertAbsent(t, cleared, "projectId", "cleared task")

	status, body := app.Patch(t, "/api/v1/tasks/"+taskID, `{"title":null}`)
	if status != http.StatusBadRequest {
		t.Errorf("null title status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Field title cannot be null" {
		t.Errorf("null title error = %q, want %q", msg, "Field title cannot be null")
	}

	status, body = app.Patch(t, "/api/v1/tasks/"+taskID, `{"title":""}`)
	if status != http.StatusBadRequest {
		t.Errorf("empty title status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Title is required" {
		t.Errorf("empty title error = %q, want %q", msg, "Title is required")
	}

	status, body = app.Patch(t, "/api/v1/tasks/"+taskID, `{"dueDate":"10-11-2026"}`)
	if status != http.StatusBadRequest {
		t.Errorf("bad due date status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Use a YYYY-MM-DD date" {
		t.Errorf("bad due date error = %q, want %q", msg, "Use a YYYY-MM-DD date")
	}

	status, _ = app.Get(t, "/api/v1/tasks/t_missing")
	if status != http.StatusNotFound {
		t.Errorf("missing task status = %d, want %d", status, http.StatusNotFound)
	}
}

// TestUpdateParentMissingAndNullBehavior ports
// update_parent_missing_and_null_behavior.
func TestUpdateParentMissingAndNullBehavior(t *testing.T) {
	app := New(t)

	_, project := app.Post(t, "/api/v1/projects", `{"name":"Parent Checks"}`)
	projectID := StringField(t, project, "id")

	_, parent := app.Post(t, "/api/v1/tasks", `{"title":"Parent","projectId":"`+projectID+`"}`)
	parentID := StringField(t, parent, "id")

	_, child := app.Post(t, "/api/v1/tasks", `{"title":"Child","parentId":"`+parentID+`"}`)
	childID := StringField(t, child, "id")

	status, body := app.Patch(t, "/api/v1/tasks/"+childID, `{"parentId":"t_missing"}`)
	if status != http.StatusBadRequest {
		t.Errorf("missing parent status = %d, want %d", status, http.StatusBadRequest)
	}
	if msg := ErrorMessage(body); msg != "Parent task not found" {
		t.Errorf("missing parent error = %q, want %q", msg, "Parent task not found")
	}

	status, cleared := app.Patch(t, "/api/v1/tasks/"+childID, `{"parentId":null}`)
	if status != http.StatusOK {
		t.Fatalf("clear parent status = %d, want %d", status, http.StatusOK)
	}
	taskAssertAbsent(t, cleared, "parentId", "cleared child")
	// Clearing the parent leaves the inherited project in place.
	if got := StringField(t, cleared, "projectId"); got != projectID {
		t.Errorf("cleared projectId = %q, want %q", got, projectID)
	}
}
