package apitest

import (
	"strings"
	"testing"
)

func TestApiTokenLifecycle(t *testing.T) {
	app := New(t)

	status, created := app.Post(t, "/api/v1/api-tokens", `{"name":"MCP"}`)
	if status != 201 {
		t.Fatalf("POST /api-tokens: status = %d, want 201 (body %v)", status, created)
	}
	pat := StringField(t, created, "token")
	if !strings.HasPrefix(pat, "sb_") {
		t.Errorf("token = %q, want an sb_ prefix", pat)
	}
	apiToken, ok := created["apiToken"].(map[string]any)
	if !ok {
		t.Fatalf("apiToken is not an object: %v", created["apiToken"])
	}
	if got := apiToken["name"]; got != "MCP" {
		t.Errorf("apiToken.name = %v, want \"MCP\"", got)
	}
	tokenID, ok := apiToken["id"].(string)
	if !ok || tokenID == "" {
		t.Fatalf("apiToken.id is not a string: %v", apiToken["id"])
	}

	status, listed := app.GetList(t, "/api/v1/api-tokens")
	if status != 200 {
		t.Fatalf("GET /api-tokens: status = %d, want 200", status)
	}
	if len(listed) != 1 {
		t.Fatalf("len(api-tokens) = %d, want 1", len(listed))
	}
	entry, ok := listed[0].(map[string]any)
	if !ok {
		t.Fatalf("api-tokens[0] is not an object: %v", listed[0])
	}
	if got := entry["id"]; got != tokenID {
		t.Errorf("api-tokens[0].id = %v, want %v", got, tokenID)
	}
	if _, present := entry["lastUsedAt"]; present {
		t.Errorf("api-tokens[0].lastUsedAt should be absent: %v", entry)
	}

	// The token authenticates exactly like a session.
	status, tasks := app.DoList(t, "GET", "/api/v1/tasks", pat, "")
	if status != 200 {
		t.Fatalf("GET /tasks with PAT: status = %d, want 200", status)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}

	status, listed = app.GetList(t, "/api/v1/api-tokens")
	if status != 200 {
		t.Fatalf("GET /api-tokens: status = %d, want 200", status)
	}
	entry, ok = listed[0].(map[string]any)
	if !ok {
		t.Fatalf("api-tokens[0] is not an object: %v", listed[0])
	}
	if _, ok := entry["lastUsedAt"].(string); !ok {
		t.Errorf("lastUsedAt not recorded: %v", listed)
	}

	// PAT logout has no session to destroy and must not fail.
	status = app.Status(t, "POST", "/api/v1/auth/logout", pat, "")
	if status != 204 {
		t.Errorf("POST /auth/logout with PAT: status = %d, want 204", status)
	}

	status = app.Delete(t, "/api/v1/api-tokens/"+tokenID)
	if status != 204 {
		t.Errorf("DELETE /api-tokens/%s: status = %d, want 204", tokenID, status)
	}

	status = app.Status(t, "GET", "/api/v1/tasks", pat, "")
	if status != 401 {
		t.Errorf("GET /tasks with revoked PAT: status = %d, want 401", status)
	}

	status, body := app.Do(t, "DELETE", "/api/v1/api-tokens/"+tokenID, app.Token, "")
	if status != 404 {
		t.Fatalf("DELETE revoked token: status = %d, want 404 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Token not found" {
		t.Errorf("error = %q, want %q", got, "Token not found")
	}
}

func TestApiTokenValidation(t *testing.T) {
	app := New(t)

	status, body := app.Post(t, "/api/v1/api-tokens", `{"name":"   "}`)
	if status != 400 {
		t.Fatalf("POST /api-tokens blank name: status = %d, want 400 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Name is required" {
		t.Errorf("error = %q, want %q", got, "Name is required")
	}

	status, body = app.Post(t, "/api/v1/api-tokens", `{"name":"`+strings.Repeat("x", 81)+`"}`)
	if status != 400 {
		t.Fatalf("POST /api-tokens long name: status = %d, want 400 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Name is too long" {
		t.Errorf("error = %q, want %q", got, "Name is too long")
	}

	status, _ = app.Post(t, "/api/v1/api-tokens", `{"name":"Tools","extra":1}`)
	if status != 400 {
		t.Errorf("POST /api-tokens with unknown field: status = %d, want 400", status)
	}

	status = app.Status(t, "GET", "/api/v1/api-tokens", "", "")
	if status != 401 {
		t.Errorf("GET /api-tokens unauthenticated: status = %d, want 401", status)
	}
}
