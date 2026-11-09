package apitest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"morrow-api/internal/config"
)

func TestAssistantFallsBackToDerivedReplies(t *testing.T) {
	app := New(t)

	status, body := app.Post(t, "/api/v1/assistant", `{ "message": "Summarize my day", "today": "2026-11-08" }`)
	if status != http.StatusOK {
		t.Errorf("summarize status = %d, want %d", status, http.StatusOK)
	}
	if provider, _ := body["provider"].(string); provider != "fallback" {
		t.Errorf("provider = %v, want %q", body["provider"], "fallback")
	}
	reply, _ := body["reply"].(string)
	if !strings.Contains(reply, "Here is your day in numbers:") {
		t.Errorf("summarize reply = %q, want it to contain %q", reply, "Here is your day in numbers:")
	}

	_, focus := app.Post(t, "/api/v1/assistant", `{ "message": "What should I focus on?", "today": "2026-11-08" }`)
	focusReply, _ := focus["reply"].(string)
	if !strings.Contains(focusReply, "Nothing is overdue") {
		t.Errorf("focus reply = %q, want it to contain %q", focusReply, "Nothing is overdue")
	}

	_, projects := app.Post(t, "/api/v1/assistant", `{ "message": "Walk through my active projects", "today": "2026-11-08" }`)
	projectsReply, _ := projects["reply"].(string)
	if !strings.Contains(projectsReply, "no active projects") {
		t.Errorf("projects reply = %q, want it to contain %q", projectsReply, "no active projects")
	}

	_, notes := app.Post(t, "/api/v1/assistant", `{ "message": "Find my notes", "today": "2026-11-08" }`)
	notesReply, _ := notes["reply"].(string)
	if !strings.Contains(notesReply, "no notes yet") {
		t.Errorf("notes reply = %q, want it to contain %q", notesReply, "no notes yet")
	}

	_, other := app.Post(t, "/api/v1/assistant", `{ "message": "hello", "today": "2026-11-08" }`)
	if got, want := other["reply"], "I can summarize your day, suggest what to focus on, or walk through a project. Try one of the suggestions."; got != want {
		t.Errorf("fallback reply = %v, want %q", got, want)
	}
}

func TestAssistantCitesSourcesAndExplainsFallback(t *testing.T) {
	app := New(t)

	status, note := app.Post(t, "/api/v1/notes", `{"title":"Warehouse runbook","content":"<p>Nightly sync at 02:00 with retry budget 3</p>"}`)
	if status != http.StatusCreated {
		t.Fatalf("create note: status = %d, want %d", status, http.StatusCreated)
	}
	noteID := StringField(t, note, "id")

	status, body := app.Post(t, "/api/v1/assistant", `{"message":"Find my notes about the warehouse","today":"2026-11-09"}`)
	if status != http.StatusOK {
		t.Fatalf("assistant: status = %d, want %d", status, http.StatusOK)
	}
	if got := StringField(t, body, "provider"); got != "fallback" {
		t.Errorf("provider = %q, want %q", got, "fallback")
	}
	if got := StringField(t, body, "reason"); got != "ai_disabled" {
		t.Errorf("reason = %q, want %q", got, "ai_disabled")
	}
	if reply := StringField(t, body, "reply"); !strings.Contains(reply, "Warehouse runbook") {
		t.Errorf("reply = %q, want the note title", reply)
	}

	sources, ok := body["sources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("sources = %v, want a non-empty array", body["sources"])
	}
	cited := false
	for _, entry := range sources {
		source, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("source is not an object: %v", entry)
		}
		if source["kind"] == "note" && source["id"] == noteID {
			cited = true
			if source["title"] != "Warehouse runbook" {
				t.Errorf("source title = %v, want %q", source["title"], "Warehouse runbook")
			}
			if source["href"] != "/knowledge/"+noteID {
				t.Errorf("source href = %v, want %q", source["href"], "/knowledge/"+noteID)
			}
		}
	}
	if !cited {
		t.Errorf("sources = %v, want note %s", sources, noteID)
	}

	// Empty source lists are arrays, never null.
	_, hello := app.Post(t, "/api/v1/assistant", `{"message":"hello","today":"2026-11-09"}`)
	if empty, ok := hello["sources"].([]any); !ok || len(empty) != 0 {
		t.Errorf("sources for unmatched reply = %v, want []", hello["sources"])
	}

	// Enabled without a key: the fallback says why.
	app.Patch(t, "/api/v1/ai", `{"enabled":true}`)
	_, missing := app.Post(t, "/api/v1/assistant", `{"message":"Summarize my day","today":"2026-11-09"}`)
	if got := StringField(t, missing, "reason"); got != "key_missing" {
		t.Errorf("reason with AI enabled and no key = %q, want %q", got, "key_missing")
	}
}

func TestAssistantParsesModelCitationsAndSendsEffort(t *testing.T) {
	var (
		noteID   string
		lastBody []byte
	)

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-flash","name":"DeepSeek Flash","context_window":1048576,"effort":{"supported_levels":["low","high","max"]}}]}`))
		case "/chat/completions":
			lastBody, _ = io.ReadAll(r.Body)
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"The runbook syncs at 02:00.\n\nSources: %s, bogus_id"}}]}`, noteID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		cfg.DeepSeekBaseURL = provider.URL
	})
	if status, _ := app.Patch(t, "/api/v1/ai", `{"apiKey":"test-key"}`); status != http.StatusOK {
		t.Fatalf("store api key: status = %d, want 200", status)
	}

	status, note := app.Post(t, "/api/v1/notes", `{"title":"Warehouse runbook","content":"<p>Nightly sync at 02:00</p>"}`)
	if status != http.StatusCreated {
		t.Fatalf("create note: status = %d, want %d", status, http.StatusCreated)
	}
	noteID = StringField(t, note, "id")

	if status, _ := app.Patch(t, "/api/v1/ai", `{"enabled":true,"reasoningEffort":"high"}`); status != http.StatusOK {
		t.Fatalf("enable ai: status = %d, want 200", status)
	}

	status, body := app.Post(t, "/api/v1/assistant", `{"message":"What does the runbook say?","today":"2026-11-09"}`)
	if status != http.StatusOK {
		t.Fatalf("assistant: status = %d, want 200", status)
	}
	if got := StringField(t, body, "provider"); got != "deepseek" {
		t.Errorf("provider = %q, want deepseek", got)
	}
	if got, present := body["reason"]; present && got != nil {
		t.Errorf("reason = %v, want absent for a model answer", got)
	}

	reply := StringField(t, body, "reply")
	if !strings.Contains(reply, "02:00") {
		t.Errorf("reply = %q, want the model text", reply)
	}
	if strings.Contains(strings.ToLower(reply), "sources:") {
		t.Errorf("reply = %q, want the Sources line stripped", reply)
	}

	sources, ok := body["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("sources = %v, want exactly the cited note", body["sources"])
	}
	source, _ := sources[0].(map[string]any)
	if source["kind"] != "note" || source["id"] != noteID || source["href"] != "/knowledge/"+noteID {
		t.Errorf("source = %v, want the note with its href", source)
	}

	if !strings.Contains(string(lastBody), `"reasoning_effort":"high"`) {
		t.Errorf("completion body = %s, want reasoning_effort high", lastBody)
	}
	if !strings.Contains(string(lastBody), `"model":"deepseek-flash"`) {
		t.Errorf("completion body = %s, want the catalogued model", lastBody)
	}
	if !strings.Contains(string(lastBody), `"max_tokens":700`) {
		t.Errorf("completion body = %s, want max_tokens 700", lastBody)
	}
}

func TestAssistantExecutesTaskActions(t *testing.T) {
	var lastBody []byte

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		lastBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Added both tasks.","tool_calls":[` +
			`{"id":"c1","type":"function","function":{"name":"create_task","arguments":"{\"title\":\"add new tables supplier\",\"project\":\"Data Warehouse Platform\",\"priority\":\"high\"}"}},` +
			`{"id":"c2","type":"function","function":{"name":"create_task","arguments":"{\"title\":\"fixing middleware request_id\",\"project\":\"Data Warehouse Platform\"}"}},` +
			`{"id":"c3","type":"function","function":{"name":"create_task","arguments":"{\"title\":\"stray\",\"project\":\"Nonexistent Space\"}"}}` +
			`]}}]}`))
	}))
	defer provider.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		cfg.DeepSeekBaseURL = provider.URL
	})
	if status, _ := app.Patch(t, "/api/v1/ai", `{"apiKey":"test-key"}`); status != http.StatusOK {
		t.Fatalf("store api key: status = %d, want 200", status)
	}

	status, project := app.Post(t, "/api/v1/projects", `{"name":"Data Warehouse Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create project: status = %d, want %d", status, http.StatusCreated)
	}
	projectID := StringField(t, project, "id")

	if status, _ := app.Patch(t, "/api/v1/ai", `{"enabled":true}`); status != http.StatusOK {
		t.Fatalf("enable ai: status = %d, want 200", status)
	}

	prompt := `{"message":"can you add task into data warehouse platform the task is - add new tables supplier - fixing middleware request_id","today":"2026-11-09"}`
	status, body := app.Post(t, "/api/v1/assistant", prompt)
	if status != http.StatusOK {
		t.Fatalf("assistant: status = %d, want 200", status)
	}
	if got := StringField(t, body, "provider"); got != "deepseek" {
		t.Errorf("provider = %q, want deepseek", got)
	}

	created, ok := body["created"].([]any)
	if !ok || len(created) != 2 {
		t.Fatalf("created = %v, want the two matched tasks", body["created"])
	}
	wantTitles := []string{"add new tables supplier", "fixing middleware request_id"}
	for index, entry := range created {
		task, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("created entry is not an object: %v", entry)
		}
		if task["title"] != wantTitles[index] {
			t.Errorf("created title = %v, want %q", task["title"], wantTitles[index])
		}
		if task["status"] != "todo" {
			t.Errorf("created status = %v, want todo", task["status"])
		}
		if task["projectId"] != projectID {
			t.Errorf("created projectId = %v, want %q", task["projectId"], projectID)
		}
		if key, _ := task["key"].(string); !strings.HasPrefix(key, "DWP-") {
			t.Errorf("created key = %v, want a DWP key", task["key"])
		}
		if _, has := task["id"]; !has {
			t.Errorf("created task has no id: %v", task)
		}
	}
	if first := created[0].(map[string]any); first["priority"] != "high" {
		t.Errorf("first priority = %v, want high", first["priority"])
	}
	if second := created[1].(map[string]any); second["priority"] != "medium" {
		t.Errorf("second priority = %v, want medium", second["priority"])
	}

	status, tasks := app.GetList(t, "/api/v1/tasks")
	if status != http.StatusOK || len(tasks) != 2 {
		t.Fatalf("tasks = %v (status %d), want the two created rows", tasks, status)
	}

	// The same request twice must not duplicate the tasks.
	_, again := app.Post(t, "/api/v1/assistant", prompt)
	if repeated, ok := again["created"].([]any); !ok || len(repeated) != 0 {
		t.Errorf("repeated created = %v, want []", again["created"])
	}

	if !strings.Contains(string(lastBody), `"tools"`) || !strings.Contains(string(lastBody), `"tool_choice":"auto"`) {
		t.Errorf("completion body = %s, want tools and tool_choice", lastBody)
	}
}
