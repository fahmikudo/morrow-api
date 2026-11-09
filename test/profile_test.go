package apitest

import "testing"

func TestProfileEmailUpdatesUserAndRejectsDuplicates(t *testing.T) {
	app := New(t)

	status, profile := app.Patch(t, "/api/v1/profile", `{"email":"new@example.com","role":null}`)
	if status != 200 {
		t.Fatalf("PATCH /profile: status = %d, want 200 (body %v)", status, profile)
	}
	if got := profile["email"]; got != "new@example.com" {
		t.Errorf("profile.email = %v, want \"new@example.com\"", got)
	}
	if _, present := profile["role"]; present {
		t.Errorf("profile.role should be absent: %v", profile)
	}

	var stored string
	if err := app.DB.Raw("select email from users where id = ?", app.UserID).Scan(&stored).Error; err != nil {
		t.Fatalf("select users.email: %v", err)
	}
	if stored != "new@example.com" {
		t.Errorf("users.email = %q, want %q", stored, "new@example.com")
	}

	app.NewUser(t, "taken@example.com", "x")

	status, body := app.Patch(t, "/api/v1/profile", `{"email":"taken@example.com"}`)
	if status != 409 {
		t.Fatalf("PATCH /profile duplicate email: status = %d, want 409 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Email already in use" {
		t.Errorf("error = %q, want %q", got, "Email already in use")
	}
	if got := body["code"]; got != "conflict" {
		t.Errorf("code = %v, want \"conflict\"", got)
	}

	status, body = app.Patch(t, "/api/v1/profile", `{"name":""}`)
	if status != 400 {
		t.Fatalf("PATCH /profile blank name: status = %d, want 400 (body %v)", status, body)
	}
	if got := ErrorMessage(body); got != "Name is required" {
		t.Errorf("error = %q, want %q", got, "Name is required")
	}
}
