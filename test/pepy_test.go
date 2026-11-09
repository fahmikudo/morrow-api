package apitest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"morrow-api/internal/config"
)

type pepyCall struct {
	method string
	path   string
	body   string
}

type fakePepy struct {
	mu       sync.Mutex
	logins   int
	failNext bool
	tasks    map[string]map[string]any
	calls    []pepyCall
}

func newFakePepy() *fakePepy {
	return &fakePepy{tasks: make(map[string]map[string]any)}
}

func (f *fakePepy) server(t *testing.T) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		f.calls = append(f.calls, pepyCall{method: r.Method, path: r.URL.Path, body: string(body)})

		unauthorized := func() bool {
			if f.failNext {
				f.failNext = false
				return true
			}
			return false
		}

		switch {
		case strings.HasSuffix(r.URL.Path, "/users/signin/sshub"):
			f.logins += 1
			claims := base64.RawURLEncoding.EncodeToString([]byte(
				fmt.Sprintf(`{"exp":%d}`, time.Now().Add(7*24*time.Hour).Unix()),
			))
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"success":true,"message":"ok","data":{"id":820,"name":"Fahmi","email":"fahmi@softwareseni.com","token":"h.%s.s"}}`,
				claims,
			)
		case strings.HasSuffix(r.URL.Path, "/tasks") && r.Method == http.MethodPost:
			if unauthorized() {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			id := "2502726"
			f.tasks[id] = payload
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"id":%s}}`, id)
		case strings.Contains(r.URL.Path, "/tasks/") && r.Method == http.MethodPut:
			if unauthorized() {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			f.tasks[id] = payload
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"id":%s}}`, id)
		case strings.Contains(r.URL.Path, "/tasks/") && r.Method == http.MethodDelete:
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			delete(f.tasks, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(handler)
}

func (f *fakePepy) callsWith(method, suffix string) []pepyCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	found := make([]pepyCall, 0)
	for _, call := range f.calls {
		if call.method == method && strings.Contains(call.path, suffix) {
			found = append(found, call)
		}
	}
	return found
}

func pepyConfig(cfg *config.Config, baseURL string) {
	cfg.PepyEnabled = true
	cfg.PepyAllowedEmails = []string{"raka@example.com"}
	cfg.PepyBaseURL = baseURL
	cfg.PepyUsername = "fahmi"
	cfg.PepyPassword = "secret"
	cfg.PepyUserID = 820
	cfg.PepyProjectID = 5674
	cfg.PepyLoginIntervalSeconds = 259200
	cfg.PepySyncSeconds = 3600
	cfg.AppEncryptionKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cfg.AppTimezone = "Asia/Jakarta"
}

func TestPepyConnectAndTaskSync(t *testing.T) {
	fake := newFakePepy()
	server := fake.server(t)
	defer server.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		pepyConfig(cfg, server.URL+"/api/v1")
	})

	status, settings := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("settings status = %d", status)
	}
	features, _ := settings["features"].(map[string]any)
	if features["softwareseni-pepy"] != true {
		t.Fatalf("pepy feature = %v, want true", features["softwareseni-pepy"])
	}

	status, connection := app.Post(t, "/api/v1/pepy/connect", "")
	if status != http.StatusOK || connection["connected"] != true || connection["accountEmail"] != "fahmi@softwareseni.com" {
		t.Fatalf("pepy connect = %d %v", status, connection)
	}

	var accessEnc string
	if err := app.DB.Raw("select access_token_enc from integration_credentials where user_id = ? and integration_id = 'softwareseni-pepy'", app.UserID).Scan(&accessEnc).Error; err != nil {
		t.Fatalf("read pepy token: %v", err)
	}
	if accessEnc == "" || strings.Contains(accessEnc, "h.") {
		t.Errorf("pepy token was not encrypted: %q", accessEnc)
	}

	status, created := app.Post(t, "/api/v1/tasks",
		`{"title":"Pepy task","dueDate":"2026-11-10","startTime":"09:00","endTime":"10:00","syncToPepy":true}`)
	if status != http.StatusCreated || created["syncToPepy"] != true {
		t.Fatalf("create synced task = %d %v", status, created)
	}
	taskID, _ := created["id"].(string)
	status, snapshot := app.Get(t, "/api/v1/snapshot")
	if status != http.StatusOK {
		t.Fatalf("snapshot status = %d", status)
	}
	syncedInSnapshot := false
	for _, item := range snapshot["tasks"].([]any) {
		taskItem := item.(map[string]any)
		if taskItem["id"] == taskID {
			syncedInSnapshot = taskItem["syncToPepy"] == true
		}
	}
	if !syncedInSnapshot {
		t.Errorf("snapshot task syncToPepy = false, want true")
	}
	creates := fake.callsWith(http.MethodPost, "/tasks")
	if len(creates) != 1 {
		t.Fatalf("pepy create calls = %d, want 1", len(creates))
	}
	if !strings.Contains(creates[0].body, `"project_id":5674`) ||
		!strings.Contains(creates[0].body, `"user_id":820`) ||
		!strings.Contains(creates[0].body, `"scheduled_start_time":"2026-11-10T02:00:00.000Z"`) {
		t.Errorf("pepy create body = %s", creates[0].body)
	}

	status, updated := app.Patch(t, "/api/v1/tasks/"+taskID, `{"title":"Pepy task edited"}`)
	if status != http.StatusOK || updated["syncToPepy"] != true {
		t.Fatalf("update synced task = %d %v", status, updated)
	}
	if calls := fake.callsWith(http.MethodPut, "/tasks/2502726"); len(calls) != 1 {
		t.Fatalf("pepy update calls = %d, want 1", len(calls))
	}

	status, done := app.Patch(t, "/api/v1/tasks/"+taskID, `{"status":"done"}`)
	if status != http.StatusOK {
		t.Fatalf("done task = %d %v", status, done)
	}
	updates := fake.callsWith(http.MethodPut, "/tasks/2502726")
	if len(updates) != 2 || !strings.Contains(updates[1].body, `"stop_time":"2026-11-10T03:00:00.000Z"`) {
		t.Errorf("done update payloads = %v", updates)
	}

	status, disabled := app.Patch(t, "/api/v1/tasks/"+taskID, `{"syncToPepy":false}`)
	if status != http.StatusOK || disabled["syncToPepy"] == true {
		t.Fatalf("disable sync = %d %v", status, disabled)
	}
	before := len(fake.callsWith(http.MethodPut, "/tasks/2502726"))
	app.Patch(t, "/api/v1/tasks/"+taskID, `{"title":"No longer synced"}`)
	if after := len(fake.callsWith(http.MethodPut, "/tasks/2502726")); after != before {
		t.Errorf("disabled task still pushed: %d → %d", before, after)
	}

	status, enabled := app.Patch(t, "/api/v1/tasks/"+taskID, `{"syncToPepy":true}`)
	if status != http.StatusOK || enabled["syncToPepy"] != true {
		t.Fatalf("re-enable sync = %d %v", status, enabled)
	}
	if after := len(fake.callsWith(http.MethodPut, "/tasks/2502726")); after != before+1 {
		t.Errorf("enabled task did not push: %d → %d", before, after)
	}

	if status := app.Status(t, http.MethodDelete, "/api/v1/tasks/"+taskID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete task = %d", status)
	}
	if calls := fake.callsWith(http.MethodDelete, "/tasks/2502726"); len(calls) != 1 {
		t.Errorf("pepy delete calls = %d, want 1", len(calls))
	}
}

func TestPepyReconciliationAndValidation(t *testing.T) {
	fake := newFakePepy()
	server := fake.server(t)
	defer server.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		pepyConfig(cfg, server.URL+"/api/v1")
	})

	status, body := app.Post(t, "/api/v1/tasks",
		`{"title":"Missing timebox","dueDate":"2026-11-10","syncToPepy":true}`)
	if status != http.StatusBadRequest ||
		ErrorMessage(body) != "Send to Pepy requires a due date, start time, and end time" {
		t.Fatalf("missing timebox = %d %q", status, ErrorMessage(body))
	}

	status, created := app.Post(t, "/api/v1/tasks",
		`{"title":"Reconcile me","dueDate":"2026-11-10","startTime":"09:00","endTime":"10:00","syncToPepy":true}`)
	if status != http.StatusCreated {
		t.Fatalf("create before connect = %d %v", status, created)
	}
	if calls := fake.callsWith(http.MethodPost, "/tasks"); len(calls) != 0 {
		t.Fatalf("pushed before connect: %d calls", len(calls))
	}

	if status, _ := app.Post(t, "/api/v1/pepy/connect", ""); status != http.StatusOK {
		t.Fatalf("connect = %d", status)
	}
	fake.mu.Lock()
	fake.failNext = true
	fake.mu.Unlock()

	if status := app.Status(t, http.MethodPost, "/api/v1/pepy/sync", app.Token, ""); status != http.StatusOK {
		t.Fatalf("sync = %d", status)
	}
	if calls := fake.callsWith(http.MethodPost, "/tasks"); len(calls) != 2 {
		t.Fatalf("reconcile create calls = %d, want 2 (401 + retry)", len(calls))
	}
	fake.mu.Lock()
	logins := fake.logins
	fake.mu.Unlock()
	if logins < 2 {
		t.Errorf("logins = %d, want a re-login after 401", logins)
	}
}

func TestPepyFeatureFlagHidesIntegration(t *testing.T) {
	app := New(t)

	status, settings := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("settings = %d", status)
	}
	features, _ := settings["features"].(map[string]any)
	if features["softwareseni-pepy"] != false {
		t.Errorf("pepy feature = %v, want false", features["softwareseni-pepy"])
	}

	status, body := app.Post(t, "/api/v1/tasks",
		`{"title":"No pepy","dueDate":"2026-11-10","startTime":"09:00","endTime":"10:00","syncToPepy":true}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "Pepy sync is not available" {
		t.Errorf("disabled pepy = %d %q", status, ErrorMessage(body))
	}
}
