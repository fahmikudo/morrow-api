package apitest

import (
	"reflect"
	"testing"
)

func TestInboxArchiveAndRestoreAreIdempotent(t *testing.T) {
	app := New(t)

	status, item := app.Post(t, "/api/v1/inbox", `{"title":"Capture","type":"idea","content":"body"}`)
	if status != 201 {
		t.Fatalf("create inbox item status = %d, want 201 (body %v)", status, item)
	}
	id := StringField(t, item, "id")

	_, first := app.Post(t, "/api/v1/inbox/"+id+"/archive", `{}`)
	_, second := app.Post(t, "/api/v1/inbox/"+id+"/archive", `{}`)
	if !reflect.DeepEqual(first["archivedAt"], second["archivedAt"]) {
		t.Errorf("archivedAt = %v on repeat, want %v", second["archivedAt"], first["archivedAt"])
	}

	_, active := app.GetList(t, "/api/v1/inbox")
	if len(active) != 0 {
		t.Errorf("active inbox items = %d, want 0", len(active))
	}
	_, archived := app.GetList(t, "/api/v1/inbox/archived")
	if len(archived) != 1 {
		t.Errorf("archived inbox items = %d, want 1", len(archived))
	}

	for range 2 {
		status, restored := app.Post(t, "/api/v1/inbox/"+id+"/restore", `{}`)
		if status != 200 {
			t.Errorf("restore status = %d, want 200", status)
		}
		if v, ok := restored["archivedAt"]; ok {
			t.Errorf("restored archivedAt = %v, want absent", v)
		}
	}

	status, body := app.Post(t, "/api/v1/inbox", `{"title":"   ","type":"note"}`)
	if status != 400 {
		t.Errorf("blank title status = %d, want 400", status)
	}
	if msg := ErrorMessage(body); msg != "Title is required" {
		t.Errorf("blank title error = %q, want %q", msg, "Title is required")
	}
	if code := ErrorCode(body); code != "validation_error" {
		t.Errorf("blank title code = %q, want %q", code, "validation_error")
	}
}
