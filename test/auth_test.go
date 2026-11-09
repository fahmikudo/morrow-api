package apitest

import (
	"testing"

	"morrow-api/internal/platform/tokens"
)

func TestLoginAndSessionFlow(t *testing.T) {
	app := New(t)

	status, body := app.Get(t, "/api/v1/auth/session")
	if status != 200 {
		t.Fatalf("GET /auth/session: status = %d, want 200 (body %v)", status, body)
	}
	profile, ok := body["profile"].(map[string]any)
	if !ok {
		t.Fatalf("GET /auth/session: profile is not an object: %v", body["profile"])
	}
	if got := profile["id"]; got != "me" {
		t.Errorf("profile.id = %v, want \"me\"", got)
	}
	if got := profile["email"]; got != "raka@example.com" {
		t.Errorf("profile.email = %v, want \"raka@example.com\"", got)
	}

	status, body = app.Do(t, "POST", "/api/v1/auth/login", "",
		`{"email":"raka@example.com","password":"wrong"}`)
	if status != 401 {
		t.Fatalf("POST /auth/login: status = %d, want 401 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Invalid email or password" {
		t.Errorf("error = %q, want %q", got, "Invalid email or password")
	}
	if got := body["code"]; got != "unauthorized" {
		t.Errorf("code = %v, want \"unauthorized\"", got)
	}

	status = app.Status(t, "POST", "/api/v1/auth/logout", app.Token, "")
	if status != 204 {
		t.Errorf("POST /auth/logout: status = %d, want 204", status)
	}

	status = app.Status(t, "GET", "/api/v1/tasks", app.Token, "")
	if status != 401 {
		t.Errorf("GET /tasks after logout: status = %d, want 401", status)
	}
}

func TestExpiredAndMalformedTokensAreRejected(t *testing.T) {
	app := New(t)

	err := app.DB.Exec(
		"insert into sessions (id, user_id, token_hash, created_at, expires_at) values ('s_old', ?, ?, now() - interval '2 days', now() - interval '1 day')",
		app.UserID,
		tokens.TokenHash("expired-token"),
	).Error
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	status := app.Status(t, "GET", "/api/v1/tasks", "expired-token", "")
	if status != 401 {
		t.Errorf("GET /tasks with expired token: status = %d, want 401", status)
	}

	status = app.Status(t, "GET", "/api/v1/tasks", "", "")
	if status != 401 {
		t.Errorf("GET /tasks without token: status = %d, want 401", status)
	}

	status, body := app.Get(t, "/api/v1/does-not-exist")
	if status != 404 {
		t.Fatalf("GET /does-not-exist: status = %d, want 404 (body %v)", status, body)
	}
	if got := body["code"]; got != "not_found" {
		t.Errorf("code = %v, want \"not_found\"", got)
	}
	if got := ErrorMessage(body); got != "Route not found" {
		t.Errorf("error = %q, want %q", got, "Route not found")
	}
}

func TestEveryEntityRouteRequiresAuth(t *testing.T) {
	app := New(t)

	paths := []string{
		"/api/v1/profile",
		"/api/v1/inbox",
		"/api/v1/inbox/archived",
		"/api/v1/tasks",
		"/api/v1/projects",
		"/api/v1/notes",
		"/api/v1/events",
		"/api/v1/areas",
		"/api/v1/habits",
		"/api/v1/habits/archived",
		"/api/v1/habit-check-ins",
		"/api/v1/settings",
		"/api/v1/snapshot",
		"/api/v1/search?q=x",
	}

	for _, path := range paths {
		status := app.Status(t, "GET", path, "", "")
		if status != 401 {
			t.Errorf("GET %s unauthenticated: status = %d, want 401", path, status)
		}
	}
}

func TestRegistrationCreatesAccountAndDefaults(t *testing.T) {
	app := New(t)

	status, body := app.Do(t, "POST", "/api/v1/auth/register", "",
		`{"name":"Ada Lovelace","email":" ADA@Example.com ","password":"correct horse"}`)
	if status != 201 {
		t.Fatalf("register status = %d, want 201 (%v)", status, body)
	}

	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("register returned no token: %v", body)
	}
	profile, _ := body["profile"].(map[string]any)
	if got := profile["email"]; got != "ada@example.com" {
		t.Errorf("profile.email = %v, want ada@example.com", got)
	}
	if got := profile["name"]; got != "Ada Lovelace" {
		t.Errorf("profile.name = %v, want Ada Lovelace", got)
	}
	if got := profile["id"]; got != "me" {
		t.Errorf("profile.id = %v, want me", got)
	}

	status, settings := app.GetAs(t, "/api/v1/settings", token)
	if status != 200 {
		t.Fatalf("settings status = %d, want 200 (%v)", status, settings)
	}
	integrations, _ := settings["integrations"].(map[string]any)
	if len(integrations) != 2 {
		t.Errorf("integrations = %d, want 2", len(integrations))
	}
	ai, _ := settings["ai"].(map[string]any)
	if ai["model"] != "deepseek-flash" {
		t.Errorf("ai.model = %v, want deepseek-flash", ai["model"])
	}

	status, reminders := app.GetAs(t, "/api/v1/reminders/settings", token)
	if status != 200 || reminders["taskTime"] != "09:00" || reminders["enabled"] != true {
		t.Errorf("reminder settings = %d %v, want enabled 09:00", status, reminders)
	}

	status, login := app.Do(t, "POST", "/api/v1/auth/login", "",
		`{"email":"ada@example.com","password":"correct horse"}`)
	if status != 200 || login["token"] == "" {
		t.Errorf("login after register = %d %v, want a token", status, login)
	}
}

func TestRegistrationRejectsDuplicateAndInvalidBodies(t *testing.T) {
	app := New(t)

	status, body := app.Do(t, "POST", "/api/v1/auth/register", "",
		`{"name":"Raka","email":"raka@example.com","password":"secondbrain"}`)
	if status != 409 || ErrorMessage(body) != "Email already in use" {
		t.Errorf("duplicate email = %d %q, want 409 Email already in use", status, ErrorMessage(body))
	}

	status, body = app.Do(t, "POST", "/api/v1/auth/register", "",
		`{"email":"not-an-email","password":"longenough"}`)
	if status != 400 || ErrorMessage(body) != "Use a valid email" {
		t.Errorf("invalid email = %d %q", status, ErrorMessage(body))
	}

	status, body = app.Do(t, "POST", "/api/v1/auth/register", "",
		`{"email":"new@example.com","password":"short"}`)
	if status != 400 || ErrorMessage(body) != "Password must be at least 8 characters" {
		t.Errorf("short password = %d %q", status, ErrorMessage(body))
	}

	status, fallback := app.Do(t, "POST", "/api/v1/auth/register", "",
		`{"email":"solo@example.com","password":"longenough"}`)
	if status != 201 {
		t.Fatalf("name fallback status = %d, want 201 (%v)", status, fallback)
	}
	profile, _ := fallback["profile"].(map[string]any)
	if profile["name"] != "solo" {
		t.Errorf("fallback name = %v, want solo", profile["name"])
	}
}
