package apitest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/reminder"
	"morrow-api/internal/module/task"
)

type recordingSender struct {
	enabled bool
	sent    int
}

func (s *recordingSender) Enabled() bool { return s.enabled }

func (s *recordingSender) Send(context.Context, reminder.Subscription, reminder.Payload) error {
	s.sent++
	return nil
}

func deliveryCount(t *testing.T, app *App) int64 {
	t.Helper()

	var count int64
	if err := app.DB.Table("reminder_deliveries").Count(&count).Error; err != nil {
		t.Fatalf("count deliveries: %v", err)
	}

	return count
}

func TestReminderDispatchDeduplicates(t *testing.T) {
	app := New(t)

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	today := time.Now().In(loc).Format("2006-01-02")

	status, created := app.Post(t, "/api/v1/tasks", fmt.Sprintf(`{"title":"Ship it","dueDate":%q}`, today))
	if status != http.StatusCreated {
		t.Fatalf("create task status = %d, want 201 (%v)", status, created)
	}

	status, updated := app.Patch(t, "/api/v1/reminders/settings", `{"taskTime":"00:00"}`)
	if status != http.StatusOK {
		t.Fatalf("update settings status = %d, want 200 (%v)", status, updated)
	}

	repo := reminder.NewRepository(app.DB)
	if err := repo.UpsertSubscription(
		context.Background(), app.UserID, "https://push.example.com/1", "p256dh", "auth", nil, time.Now().UTC(),
	); err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}

	sender := &recordingSender{enabled: true}
	scheduler := reminder.NewScheduler(
		app.DB,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		repo,
		task.NewRepository(app.DB),
		event.NewRepository(app.DB),
		habit.NewRepository(app.DB),
		profile.NewRepository(app.DB),
		sender,
		time.Minute,
		"Asia/Jakarta",
	)

	run := func() {
		t.Helper()
		if err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatalf("run scheduler: %v", err)
		}
	}

	run()
	if got := deliveryCount(t, app); got != 1 {
		t.Fatalf("deliveries after first run = %d, want 1", got)
	}
	if sender.sent != 1 {
		t.Fatalf("pushes after first run = %d, want 1", sender.sent)
	}

	run()
	if got := deliveryCount(t, app); got != 1 {
		t.Fatalf("deliveries after second run = %d, want 1", got)
	}
	if sender.sent != 1 {
		t.Fatalf("pushes after second run = %d, want 1", sender.sent)
	}

	status, list := app.GetList(t, "/api/v1/reminders")
	if status != http.StatusOK || len(list) != 1 {
		t.Fatalf("reminders = %d status %d, want one 200", len(list), status)
	}

	item, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("reminder is not an object: %v", list[0])
	}
	if item["kind"] != "task" || item["title"] != "Task due today" || item["href"] != "/tasks" {
		t.Errorf("reminder = %v", item)
	}
	if body, _ := item["body"].(string); len(body) == 0 {
		t.Errorf("reminder body is empty: %v", item["body"])
	}

	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("reminder id is missing: %v", item["id"])
	}

	if status := app.Status(t, http.MethodPost, "/api/v1/reminders/"+id+"/dismiss", app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, want 204", status)
	}

	status, list = app.GetList(t, "/api/v1/reminders")
	if status != http.StatusOK || len(list) != 0 {
		t.Fatalf("reminders after dismiss = %d status %d, want empty 200", len(list), status)
	}

	status, body := app.Post(t, "/api/v1/reminders/"+id+"/dismiss", "")
	if status != http.StatusNotFound || ErrorMessage(body) != "Reminder not found" {
		t.Errorf("second dismiss = %d %q, want 404 Reminder not found", status, ErrorMessage(body))
	}
}

func TestReminderSettingsValidation(t *testing.T) {
	app := New(t)

	status, body := app.Patch(t, "/api/v1/reminders/settings", `{"taskTime":"9:00"}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "Use an HH:mm time" {
		t.Errorf("invalid time = %d %q", status, ErrorMessage(body))
	}

	status, body = app.Patch(t, "/api/v1/reminders/settings", `{"eventLeadMinutes":1441}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "Lead time must be between 0 and 1440" {
		t.Errorf("invalid lead = %d %q", status, ErrorMessage(body))
	}

	status, updated := app.Patch(
		t, "/api/v1/reminders/settings",
		`{"enabled":false,"taskTime":"07:30","eventLeadMinutes":30}`,
	)
	if status != http.StatusOK {
		t.Fatalf("valid patch status = %d, want 200 (%v)", status, updated)
	}
	if updated["enabled"] != false || updated["taskTime"] != "07:30" || updated["eventLeadMinutes"] != float64(30) {
		t.Errorf("patched settings = %v", updated)
	}

	status, current := app.Get(t, "/api/v1/reminders/settings")
	if status != http.StatusOK {
		t.Fatalf("get settings status = %d, want 200", status)
	}
	if current["taskTime"] != "07:30" || current["eventLeadMinutes"] != float64(30) {
		t.Errorf("round-tripped settings = %v", current)
	}
}

func TestPushSubscriptions(t *testing.T) {
	app := New(t)

	status, config := app.Get(t, "/api/v1/push/config")
	if status != http.StatusOK || config["enabled"] != false || config["publicKey"] != "" {
		t.Fatalf("push config = %d %v, want disabled with an empty key", status, config)
	}

	status, body := app.Post(t, "/api/v1/push/subscriptions", `{"endpoint":"","keys":{"p256dh":"k","auth":"a"}}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "Endpoint is required" {
		t.Errorf("empty endpoint = %d %q", status, ErrorMessage(body))
	}

	payload := `{"endpoint":"https://push.example.com/1","keys":{"p256dh":"key","auth":"secret"}}`
	if status := app.Status(t, http.MethodPost, "/api/v1/push/subscriptions", app.Token, payload); status != http.StatusNoContent {
		t.Fatalf("subscribe status = %d, want 204", status)
	}
	if status := app.Status(t, http.MethodPost, "/api/v1/push/subscriptions", app.Token, payload); status != http.StatusNoContent {
		t.Fatalf("repeat subscribe status = %d, want 204", status)
	}

	var count int64
	if err := app.DB.Table("push_subscriptions").Where("endpoint = ?", "https://push.example.com/1").Count(&count).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscription rows = %d, want 1", count)
	}

	deleteBody := `{"endpoint":"https://push.example.com/1"}`
	if status := app.Status(t, http.MethodDelete, "/api/v1/push/subscriptions", app.Token, deleteBody); status != http.StatusNoContent {
		t.Fatalf("unsubscribe status = %d, want 204", status)
	}
	if status := app.Status(t, http.MethodDelete, "/api/v1/push/subscriptions", app.Token, deleteBody); status != http.StatusNoContent {
		t.Fatalf("repeat unsubscribe status = %d, want 204", status)
	}

	if err := app.DB.Table("push_subscriptions").Count(&count).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 0 {
		t.Fatalf("subscription rows after delete = %d, want 0", count)
	}
}
