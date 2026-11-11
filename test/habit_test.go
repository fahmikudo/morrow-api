package apitest

import (
	"reflect"
	"testing"
)

func TestHabitDeleteCascadesCheckInsAndCheckInIsIdempotent(t *testing.T) {
	app := New(t)

	status, habit := app.Post(t, "/api/v1/habits", `{"title":"Read","cadence":"daily","target":1,"startDate":"2026-11-01"}`)
	if status != 201 {
		t.Fatalf("create habit status = %d, want 201 (body %v)", status, habit)
	}
	if want := map[string]any{"kind": "flexible"}; !reflect.DeepEqual(habit["schedule"], want) {
		t.Errorf("schedule = %v, want %v", habit["schedule"], want)
	}
	habitID := StringField(t, habit, "id")

	status, first := app.Put(t, "/api/v1/habits/"+habitID+"/check-ins/2026-11-08")
	if status != 200 {
		t.Fatalf("put check-in status = %d, want 200 (body %v)", status, first)
	}
	_, second := app.Put(t, "/api/v1/habits/"+habitID+"/check-ins/2026-11-08")
	if !reflect.DeepEqual(first["id"], second["id"]) {
		t.Errorf("check-in id = %v on repeat, want %v", second["id"], first["id"])
	}

	status, body := app.Put(t, "/api/v1/habits/"+habitID+"/check-ins/8-11-2026")
	if status != 400 {
		t.Errorf("bad date status = %d, want 400", status)
	}
	if msg := ErrorMessage(body); msg != "Use a YYYY-MM-DD date for check-ins" {
		t.Errorf("bad date error = %q, want %q", msg, "Use a YYYY-MM-DD date for check-ins")
	}

	status, body = app.Put(t, "/api/v1/habits/h_missing/check-ins/2026-11-08")
	if status != 404 {
		t.Errorf("missing habit status = %d, want 404", status)
	}
	if msg := ErrorMessage(body); msg != "Habit not found" {
		t.Errorf("missing habit error = %q, want %q", msg, "Habit not found")
	}

	if status := app.Delete(t, "/api/v1/habits/"+habitID); status != 204 {
		t.Errorf("delete habit status = %d, want 204", status)
	}

	_, checkIns := app.GetList(t, "/api/v1/habit-check-ins")
	if len(checkIns) != 0 {
		t.Errorf("check-ins after delete = %d, want 0", len(checkIns))
	}
}

func TestHabitReminderSettingsRoundTrip(t *testing.T) {
	app := New(t)

	status, habit := app.Post(t, "/api/v1/habits",
		`{"title":"Walk","startTime":"08:00","reminderEnabled":false,"reminderLeadMinutes":15}`)
	if status != 201 {
		t.Fatalf("create habit status = %d, want 201 (body %v)", status, habit)
	}
	if habit["reminderEnabled"] != false {
		t.Errorf("reminderEnabled = %v, want false", habit["reminderEnabled"])
	}
	if habit["reminderLeadMinutes"] != float64(15) {
		t.Errorf("reminderLeadMinutes = %v, want 15", habit["reminderLeadMinutes"])
	}
	habitID := StringField(t, habit, "id")

	status, fetched := app.Get(t, "/api/v1/habits/"+habitID)
	if status != 200 {
		t.Fatalf("get habit status = %d, want 200", status)
	}
	if fetched["reminderEnabled"] != false || fetched["reminderLeadMinutes"] != float64(15) {
		t.Errorf("fetched reminder settings = %v/%v, want false/15",
			fetched["reminderEnabled"], fetched["reminderLeadMinutes"])
	}

	status, updated := app.Patch(t, "/api/v1/habits/"+habitID,
		`{"reminderEnabled":null,"reminderLeadMinutes":null}`)
	if status != 200 {
		t.Fatalf("patch habit status = %d, want 200 (body %v)", status, updated)
	}
	if _, ok := updated["reminderEnabled"]; ok {
		t.Errorf("reminderEnabled = %v, want absent after clear", updated["reminderEnabled"])
	}
	if _, ok := updated["reminderLeadMinutes"]; ok {
		t.Errorf("reminderLeadMinutes = %v, want absent after clear", updated["reminderLeadMinutes"])
	}

	status, body := app.Patch(t, "/api/v1/habits/"+habitID, `{"reminderLeadMinutes":1441}`)
	if status != 400 {
		t.Errorf("out-of-range lead status = %d, want 400", status)
	}
	if msg := ErrorMessage(body); msg != "Reminder lead must be between 0 and 1440 minutes" {
		t.Errorf("out-of-range lead error = %q", msg)
	}
}

func TestHabitArchiveAndRestoreAreIdempotent(t *testing.T) {
	app := New(t)

	_, habit := app.Post(t, "/api/v1/habits", `{"title":"Stretch","cadence":"daily","startDate":"2026-11-01"}`)
	habitID := StringField(t, habit, "id")

	_, first := app.Post(t, "/api/v1/habits/"+habitID+"/archive", `{}`)
	_, second := app.Post(t, "/api/v1/habits/"+habitID+"/archive", `{}`)
	if _, ok := first["archivedAt"].(string); !ok {
		t.Errorf("archivedAt = %v, want a string", first["archivedAt"])
	}
	if !reflect.DeepEqual(first["archivedAt"], second["archivedAt"]) {
		t.Errorf("archivedAt = %v on repeat, want %v", second["archivedAt"], first["archivedAt"])
	}

	_, active := app.GetList(t, "/api/v1/habits")
	if len(active) != 0 {
		t.Errorf("active habits = %d, want 0", len(active))
	}
	_, archived := app.GetList(t, "/api/v1/habits/archived")
	if len(archived) != 1 {
		t.Errorf("archived habits = %d, want 1", len(archived))
	}

	for range 2 {
		status, restored := app.Post(t, "/api/v1/habits/"+habitID+"/restore", `{}`)
		if status != 200 {
			t.Errorf("restore status = %d, want 200", status)
		}
		if v, ok := restored["archivedAt"]; ok {
			t.Errorf("restored archivedAt = %v, want absent", v)
		}
	}
}
