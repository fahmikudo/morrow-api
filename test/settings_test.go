package apitest

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestIntegrationStateMachine(t *testing.T) {
	app := New(t)

	status, settings := app.Get(t, "/api/v1/settings")
	if status != 200 {
		t.Fatalf("settings status = %d, want 200", status)
	}
	integrations, ok := settings["integrations"].(map[string]any)
	if !ok {
		t.Fatalf("settings.integrations is not an object: %v", settings["integrations"])
	}
	wantGoogle := map[string]any{"enabled": true, "connected": false}
	if got := integrations["google-calendar"]; !reflect.DeepEqual(got, wantGoogle) {
		t.Errorf("google-calendar = %v, want %v", got, wantGoogle)
	}

	ai, ok := settings["ai"].(map[string]any)
	if !ok {
		t.Fatalf("settings.ai is not an object: %v", settings["ai"])
	}
	if got := ai["provider"]; got != "deepseek" {
		t.Errorf("ai.provider = %v, want deepseek", got)
	}
	if got := ai["model"]; got != "deepseek-flash" {
		t.Errorf("ai.model = %v, want deepseek-flash", got)
	}
	if got := ai["keyConfigured"]; got != false {
		t.Errorf("ai.keyConfigured = %v, want false", got)
	}
	if got := settings["googleCalendarConfigured"]; got != false {
		t.Errorf("googleCalendarConfigured = %v, want false", got)
	}
	if _, present := ai["localBaseUrl"]; present {
		t.Errorf("ai.localBaseUrl should be absent, got %v", ai["localBaseUrl"])
	}

	status, body := app.Post(t, "/api/v1/integrations/nope/connect", `{}`)
	if status != 404 {
		t.Errorf("unknown integration connect status = %d, want 404", status)
	}
	if got := ErrorMessage(body); got != "Integration not found" {
		t.Errorf("unknown integration error = %q, want %q", got, "Integration not found")
	}

	status, connected := app.Post(t, "/api/v1/integrations/google-calendar/connect", `{}`)
	if status != 200 {
		t.Fatalf("connect status = %d, want 200", status)
	}
	if got := connected["connected"]; got != true {
		t.Errorf("connected = %v, want true", got)
	}
	firstSync, ok := connected["lastSyncAt"].(string)
	if !ok {
		t.Fatalf("connect lastSyncAt should be a string, got %v", connected["lastSyncAt"])
	}

	time.Sleep(10 * time.Millisecond)

	status, synced := app.Post(t, "/api/v1/integrations/google-calendar/sync", `{}`)
	if status != 200 {
		t.Fatalf("sync status = %d, want 200", status)
	}
	syncedAt, ok := synced["lastSyncAt"].(string)
	if !ok {
		t.Fatalf("sync lastSyncAt should be a string, got %v", synced["lastSyncAt"])
	}
	if syncedAt == firstSync {
		t.Errorf("sync lastSyncAt = %q, want a value different from %q", syncedAt, firstSync)
	}

	_, disabled := app.Patch(t, "/api/v1/integrations/google-calendar", `{"enabled": false}`)
	if got := disabled["enabled"]; got != false {
		t.Errorf("disabled enabled = %v, want false", got)
	}
	if got := disabled["connected"]; got != false {
		t.Errorf("disabled connected = %v, want false", got)
	}

	status, body = app.Post(t, "/api/v1/integrations/google-calendar/connect", `{}`)
	if status != 400 {
		t.Errorf("connect while disabled status = %d, want 400", status)
	}
	if got := ErrorMessage(body); got != "Integration is disabled" {
		t.Errorf("connect while disabled error = %q, want %q", got, "Integration is disabled")
	}

	app.Patch(t, "/api/v1/integrations/google-calendar", `{"enabled": true}`)

	status, body = app.Post(t, "/api/v1/integrations/google-calendar/sync", `{}`)
	if status != 400 {
		t.Errorf("sync while disconnected status = %d, want 400", status)
	}
	if got := ErrorMessage(body); got != "Integration is not connected" {
		t.Errorf("sync while disconnected error = %q, want %q", got, "Integration is not connected")
	}

	_, disconnected := app.Post(t, "/api/v1/integrations/google-calendar/disconnect", `{}`)
	if got := disconnected["connected"]; got != false {
		t.Errorf("disconnected connected = %v, want false", got)
	}
	if got := disconnected["lastSyncAt"]; got != synced["lastSyncAt"] {
		t.Errorf("disconnected lastSyncAt = %v, want %v", got, synced["lastSyncAt"])
	}
}

func TestAiSettingsAreDeepSeekOnly(t *testing.T) {
	app := New(t)

	status, settings := app.Get(t, "/api/v1/settings")
	if status != 200 {
		t.Fatalf("settings status = %d, want 200", status)
	}
	ai, ok := settings["ai"].(map[string]any)
	if !ok {
		t.Fatalf("settings.ai is not an object: %v", settings["ai"])
	}
	if got := ai["provider"]; got != "deepseek" {
		t.Errorf("ai.provider = %v, want deepseek", got)
	}
	if got := ai["model"]; got != "deepseek-flash" {
		t.Errorf("ai.model = %v, want deepseek-flash", got)
	}
	if got := ai["keyConfigured"]; got != false {
		t.Errorf("ai.keyConfigured = %v, want false", got)
	}

	status, updated := app.Patch(t, "/api/v1/ai", `{"enabled": true, "model": "deepseek-v4-pro"}`)
	if status != 200 {
		t.Fatalf("patch ai status = %d, want 200", status)
	}
	if got := updated["enabled"]; got != true {
		t.Errorf("updated enabled = %v, want true", got)
	}
	if got := updated["provider"]; got != "deepseek" {
		t.Errorf("updated provider = %v, want deepseek", got)
	}
	if got := updated["model"]; got != "deepseek-v4-pro" {
		t.Errorf("updated model = %v, want deepseek-v4-pro", got)
	}

	status, body := app.Patch(t, "/api/v1/ai", `{"model": "gpt-4o-mini"}`)
	if status != 400 {
		t.Errorf("unknown model status = %d, want 400", status)
	}
	if got := ErrorMessage(body); got != "Unknown model" {
		t.Errorf("unknown model error = %q, want %q", got, "Unknown model")
	}

	// Provider selection and local endpoints are gone: unknown fields are rejected.
	for _, payload := range []string{
		`{"provider": "openai"}`,
		`{"localBaseUrl": "http://localhost:11434/v1"}`,
	} {
		status, _ := app.Patch(t, "/api/v1/ai", payload)
		if status != 400 {
			t.Errorf("patch ai %s status = %d, want 400", payload, status)
		}
	}

	status, result := app.Post(t, "/api/v1/ai/test", `{}`)
	if status != 200 {
		t.Fatalf("ai test status = %d, want 200", status)
	}
	if got := result["ok"]; got != false {
		t.Errorf("ai test ok = %v, want false", got)
	}
	if got := result["status"]; got != "error" {
		t.Errorf("ai test status field = %v, want error", got)
	}
	if got := result["message"]; got != "DeepSeek API key is not configured" {
		t.Errorf("ai test message = %v, want %q", got, "DeepSeek API key is not configured")
	}
}

func TestAiModelCatalogueAndEffort(t *testing.T) {
	app := New(t)

	status, settings := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("GET /settings: status = %d, want 200", status)
	}

	models, ok := settings["aiModels"].([]any)
	if !ok || len(models) == 0 {
		t.Fatalf("aiModels = %v, want a non-empty array", settings["aiModels"])
	}
	ids := make(map[string]bool, len(models))
	for _, entry := range models {
		model, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("aiModels entry is not an object: %v", entry)
		}
		id, _ := model["id"].(string)
		ids[id] = true
	}
	for _, id := range []string{"deepseek-flash", "deepseek-v4-pro"} {
		if !ids[id] {
			t.Errorf("aiModels = %v, want %q", ids, id)
		}
	}
	if got := settings["aiEffort"]; got != nil {
		t.Errorf("aiEffort = %v, want null", got)
	}

	status, _ = app.Patch(t, "/api/v1/ai", `{"reasoningEffort":"low"}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH effort: status = %d, want 200", status)
	}
	_, settings = app.Get(t, "/api/v1/settings")
	if got := settings["aiEffort"]; got != "low" {
		t.Errorf("aiEffort = %v, want %q", got, "low")
	}

	status, body := app.Patch(t, "/api/v1/ai", `{"reasoningEffort":"maximal"}`)
	if status != http.StatusBadRequest {
		t.Errorf("invalid effort: status = %d, want 400", status)
	}
	if got := ErrorMessage(body); got != "Unknown reasoning effort" {
		t.Errorf("invalid effort error = %q, want %q", got, "Unknown reasoning effort")
	}

	status, _ = app.Patch(t, "/api/v1/ai", `{"reasoningEffort":null}`)
	if status != http.StatusOK {
		t.Fatalf("clear effort: status = %d, want 200", status)
	}
	_, settings = app.Get(t, "/api/v1/settings")
	if got := settings["aiEffort"]; got != nil {
		t.Errorf("aiEffort after clear = %v, want null", got)
	}

	// Legacy aliases keep working so rows written before the catalogue change save.
	status, _ = app.Patch(t, "/api/v1/ai", `{"model":"deepseek-chat"}`)
	if status != http.StatusOK {
		t.Errorf("legacy model: status = %d, want 200", status)
	}

	status, _ = app.Patch(t, "/api/v1/ai", `{"model":"deepseek-flash"}`)
	if status != http.StatusOK {
		t.Errorf("catalogue model: status = %d, want 200", status)
	}
}

func TestAiUserKeyIsWriteOnly(t *testing.T) {
	app := New(t)

	status, updated := app.Patch(t, "/api/v1/ai", `{"apiKey":"  sk-personal  "}`)
	if status != http.StatusOK {
		t.Fatalf("store key status = %d, want 200 (%v)", status, updated)
	}
	if got := updated["keyConfigured"]; got != true {
		t.Errorf("keyConfigured = %v, want true", got)
	}
	if _, present := updated["apiKey"]; present {
		t.Errorf("response exposed apiKey: %v", updated["apiKey"])
	}
	if got := updated["status"]; got != "untested" {
		t.Errorf("status after key change = %v, want untested", got)
	}

	var stored string
	if err := app.DB.Raw("select api_key from ai_settings where user_id = ?", app.UserID).Scan(&stored).Error; err != nil {
		t.Fatalf("read stored key: %v", err)
	}
	if stored != "sk-personal" {
		t.Errorf("stored key = %q, want trimmed sk-personal", stored)
	}

	status, settings := app.Get(t, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("settings status = %d, want 200", status)
	}
	ai, _ := settings["ai"].(map[string]any)
	if ai["keyConfigured"] != true {
		t.Errorf("settings keyConfigured = %v, want true", ai["keyConfigured"])
	}
	if _, present := ai["apiKey"]; present {
		t.Errorf("settings exposed apiKey: %v", ai["apiKey"])
	}

	status, body := app.Patch(t, "/api/v1/ai", `{"apiKey":"   "}`)
	if status != http.StatusBadRequest || ErrorMessage(body) != "API key cannot be empty" {
		t.Errorf("blank key = %d %q, want 400 API key cannot be empty", status, ErrorMessage(body))
	}

	status, cleared := app.Patch(t, "/api/v1/ai", `{"apiKey":null}`)
	if status != http.StatusOK || cleared["keyConfigured"] != false {
		t.Errorf("clear key = %d %v, want keyConfigured false", status, cleared)
	}
}
