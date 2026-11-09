package apitest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"morrow-api/internal/config"
	"morrow-api/internal/platform/google"
)

type recordedCall struct {
	method string
	path   string
	body   string
}

type fakeCalendar struct {
	mu       sync.Mutex
	events   map[string]google.Event
	updates  []google.Event
	calls    []recordedCall
	nextSync int
}

func newFakeCalendar() *fakeCalendar {
	return &fakeCalendar{
		events: map[string]google.Event{
			"g1": {
				ID:          "g1",
				Status:      "confirmed",
				Summary:     "Google sync",
				Description: "Imported from Google",
				Updated:     "2026-11-09T01:00:00Z",
				Etag:        "etag-1",
				Start:       google.EventDateTime{DateTime: "2026-11-10T09:00:00+07:00"},
				End:         google.EventDateTime{DateTime: "2026-11-10T10:00:00+07:00"},
			},
		},
	}
}

func (f *fakeCalendar) server(t *testing.T) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		f.calls = append(f.calls, recordedCall{method: r.Method, path: r.URL.Path, body: string(body)})

		switch {
		case r.URL.Path == "/token":
			payload, _ := base64.RawURLEncoding.DecodeString("eyJlbWFpbCI6InJhdmlAZXhhbXBsZS5jb20ifQ")
			w.Header().Set("content-type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600,"token_type":"Bearer","scope":"calendar","id_token":"%s"}`,
				"header."+base64.RawURLEncoding.EncodeToString(payload)+".signature",
			)
		case r.URL.Path == "/revoke":
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/users/me/calendarList"):
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"primary","summary":"Primary","primary":true,"accessRole":"owner"},{"id":"work@example.com","summary":"Work","primary":false,"accessRole":"writer"}]}`))
		case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodGet:
			items := make([]google.Event, 0)
			if r.URL.Query().Get("syncToken") == "" && strings.Contains(r.URL.Path, "/calendars/primary/") {
				for _, event := range f.events {
					items = append(items, event)
				}
			} else if len(f.updates) > 0 {
				items = append(items, f.updates...)
				f.updates = nil
			}

			f.nextSync += 1
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items":         items,
				"nextSyncToken": fmt.Sprintf("sync-%d", f.nextSync),
			})
		case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodPost:
			var payload google.EventPayload
			_ = json.Unmarshal(body, &payload)
			event := google.Event{
				ID:          fmt.Sprintf("g-new-%d", len(f.events)+1),
				Status:      "confirmed",
				Summary:     payload.Summary,
				Description: payload.Description,
				Start:       payload.Start,
				End:         payload.End,
				Updated:     "2026-11-09T03:00:00Z",
				Etag:        "etag-new",
			}
			f.events[event.ID] = event
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(event)
		case strings.Contains(r.URL.Path, "/events/") && r.Method == http.MethodPut:
			var payload google.EventPayload
			_ = json.Unmarshal(body, &payload)
			parts := strings.Split(r.URL.Path, "/events/")
			id := parts[len(parts)-1]
			event := google.Event{
				ID:          id,
				Status:      "confirmed",
				Summary:     payload.Summary,
				Description: payload.Description,
				Start:       payload.Start,
				End:         payload.End,
				Updated:     "2026-11-09T04:00:00Z",
				Etag:        "etag-updated",
			}
			f.events[id] = event
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(event)
		case strings.Contains(r.URL.Path, "/events/") && r.Method == http.MethodDelete:
			parts := strings.Split(r.URL.Path, "/events/")
			id := parts[len(parts)-1]
			delete(f.events, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(handler)
}

func (f *fakeCalendar) callsWith(method, suffix string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	found := make([]recordedCall, 0)
	for _, call := range f.calls {
		if call.method == method && strings.Contains(call.path, suffix) {
			found = append(found, call)
		}
	}
	return found
}

func TestGoogleCalendarOAuthAndTwoWaySync(t *testing.T) {
	fake := newFakeCalendar()
	server := fake.server(t)
	defer server.Close()

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	app := NewWithConfig(t, func(cfg *config.Config) {
		cfg.GoogleClientID = "client"
		cfg.GoogleClientSecret = "secret"
		cfg.GoogleRedirectURL = "http://localhost/callback"
		cfg.GoogleAuthorizeURL = server.URL + "/authorize"
		cfg.GoogleTokenURL = server.URL + "/token"
		cfg.GoogleAPIBaseURL = server.URL + "/calendar/v3"
		cfg.GoogleRevokeURL = server.URL + "/revoke"
		cfg.AppEncryptionKey = key
	})

	status, settings := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK || settings["googleCalendarConfigured"] != true {
		t.Fatalf("googleCalendarConfigured = %v, want true", settings["googleCalendarConfigured"])
	}

	if status := app.Status(t, http.MethodPatch, "/api/v1/profile", app.Token, `{"timezone":"Asia/Jakarta"}`); status != http.StatusOK {
		t.Fatalf("set timezone: status = %d", status)
	}

	status, body := app.Post(t, "/api/v1/google-calendar/oauth/start", "")
	if status != http.StatusOK {
		t.Fatalf("oauth start status = %d, want 200 (%v)", status, body)
	}
	authorization, _ := body["authorizationUrl"].(string)
	parsed, err := url.Parse(authorization)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("authorization url has no state")
	}

	status, _ = app.DoWithHeaders(http.MethodGet,
		"/api/v1/integrations/google-calendar/oauth/callback?code=code-1&state="+url.QueryEscape(state), "", "", nil)
	if status != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", status)
	}

	status, events := app.GetList(t, "/api/v1/events")
	if status != http.StatusOK || len(events) != 1 {
		t.Fatalf("events = %d status %d, want one imported event", len(events), status)
	}
	imported, _ := events[0].(map[string]any)
	eventID, _ := imported["id"].(string)
	if imported["title"] != "Google sync" || imported["source"] != "google" {
		t.Errorf("imported event = %v", imported)
	}
	if imported["date"] != "2026-11-10" || imported["startTime"] != "09:00" {
		t.Errorf("imported time = %v %v, want 2026-11-10 09:00", imported["date"], imported["startTime"])
	}

	var accessEnc string
	if err := app.DB.Raw("select access_token_enc from integration_credentials where user_id = ?", app.UserID).Scan(&accessEnc).Error; err != nil {
		t.Fatalf("read credential: %v", err)
	}
	if accessEnc == "" || accessEnc == "access-1" {
		t.Errorf("access token was not encrypted: %q", accessEnc)
	}

	status, updated := app.Patch(t, "/api/v1/events/"+eventID, `{"title":"Edited locally"}`)
	if status != http.StatusOK || updated["title"] != "Edited locally" {
		t.Fatalf("local edit = %d %v", status, updated)
	}
	if calls := fake.callsWith(http.MethodPut, "/events/g1"); len(calls) != 1 {
		t.Errorf("google update calls = %d, want 1", len(calls))
	}

	status, created := app.Post(t, "/api/v1/events",
		`{"title":"Local event","date":"2026-11-11","startTime":"11:00","endTime":"12:00"}`)
	if status != http.StatusCreated {
		t.Fatalf("local create = %d %v", status, created)
	}
	createdID, _ := created["id"].(string)
	if calls := fake.callsWith(http.MethodPost, "/events"); len(calls) != 1 {
		t.Errorf("google insert calls = %d, want 1", len(calls))
	}

	if status := app.Status(t, http.MethodPost, "/api/v1/google-calendar/sync", app.Token, ""); status != http.StatusOK {
		t.Errorf("sync status = %d, want 200", status)
	}

	status, items := app.GetList(t, "/api/v1/google-calendar/calendars")
	if status != http.StatusOK || len(items) != 2 {
		t.Fatalf("calendars = %d status %d, want 2", len(items), status)
	}
	status, selected := app.Do(t, http.MethodPut, "/api/v1/google-calendar/calendar", app.Token,
		`{"calendarId":"work@example.com"}`)
	if status != http.StatusOK || selected["calendarId"] != "work@example.com" {
		t.Fatalf("select calendar = %d %v", status, selected)
	}

	status, current := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("settings status = %d", status)
	}
	integrations, _ := current["integrations"].(map[string]any)
	googleState, _ := integrations["google-calendar"].(map[string]any)
	if googleState["connected"] != true || googleState["accountEmail"] != "ravi@example.com" {
		t.Errorf("google connection = %v", googleState)
	}

	if status := app.Status(t, http.MethodDelete, "/api/v1/events/"+createdID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete local event = %d, want 204", status)
	}
	if calls := fake.callsWith(http.MethodDelete, "/events/g-new"); len(calls) != 1 {
		t.Errorf("google delete calls = %d, want 1", len(calls))
	}

	if status := app.Status(t, http.MethodPost, "/api/v1/google-calendar/disconnect", app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("disconnect = %d, want 204", status)
	}

	status, events = app.GetList(t, "/api/v1/events")
	if status != http.StatusOK || len(events) != 1 {
		t.Fatalf("events after disconnect = %d status %d, want the imported event kept", len(events), status)
	}
	kept, _ := events[0].(map[string]any)
	if kept["source"] != "local" {
		t.Errorf("kept event source = %v, want local", kept["source"])
	}
}

func TestGoogleCalendarLastWriteWins(t *testing.T) {
	fake := newFakeCalendar()
	server := fake.server(t)
	defer server.Close()

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	app := NewWithConfig(t, func(cfg *config.Config) {
		cfg.GoogleClientID = "client"
		cfg.GoogleClientSecret = "secret"
		cfg.GoogleRedirectURL = "http://localhost/callback"
		cfg.GoogleAuthorizeURL = server.URL + "/authorize"
		cfg.GoogleTokenURL = server.URL + "/token"
		cfg.GoogleAPIBaseURL = server.URL + "/calendar/v3"
		cfg.GoogleRevokeURL = server.URL + "/revoke"
		cfg.AppEncryptionKey = key
	})

	status, body := app.Post(t, "/api/v1/google-calendar/oauth/start", "")
	if status != http.StatusOK {
		t.Fatalf("oauth start = %d %v", status, body)
	}
	authorization, _ := body["authorizationUrl"].(string)
	parsed, _ := url.Parse(authorization)
	state := parsed.Query().Get("state")

	if status, _ := app.DoWithHeaders(http.MethodGet,
		"/api/v1/integrations/google-calendar/oauth/callback?code=code-1&state="+url.QueryEscape(state), "", "", nil); status != http.StatusFound {
		t.Fatalf("callback status = %d", status)
	}

	_, events := app.GetList(t, "/api/v1/events")
	imported, _ := events[0].(map[string]any)
	eventID, _ := imported["id"].(string)

	fake.mu.Lock()
	remote := fake.events["g1"]
	remote.Summary = "Remote changed"
	remote.Updated = time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339)
	fake.updates = []google.Event{remote}
	fake.mu.Unlock()

	if status := app.Status(t, http.MethodPost, "/api/v1/google-calendar/sync", app.Token, ""); status != http.StatusOK {
		t.Fatalf("sync status = %d", status)
	}
	_, events = app.GetList(t, "/api/v1/events")
	current, _ := events[0].(map[string]any)
	if current["title"] != "Remote changed" {
		t.Errorf("remote update did not win: %v", current["title"])
	}

	if status, _ := app.Patch(t, "/api/v1/events/"+eventID, `{"title":"Local wins"}`); status != http.StatusOK {
		t.Fatalf("local edit status = %d", status)
	}
	if status := app.Status(t, http.MethodPost, "/api/v1/google-calendar/sync", app.Token, ""); status != http.StatusOK {
		t.Fatalf("second sync status = %d", status)
	}
	_, events = app.GetList(t, "/api/v1/events")
	current, _ = events[0].(map[string]any)
	if current["title"] != "Local wins" {
		t.Errorf("local edit was overwritten: %v", current["title"])
	}
}
