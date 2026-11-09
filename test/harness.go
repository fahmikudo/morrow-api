// Package apitest builds an isolated, migrated, seeded database per test and
// drives the real router through httptest. No listener, no shared state.
package apitest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"

	"morrow-api/internal/bootstrap"
	"morrow-api/internal/config"
	"morrow-api/internal/database"
	"morrow-api/internal/module/auth"
	"morrow-api/migrations"
)

// fallbackDSN matches the compose stack and the Makefile default.
const fallbackDSN = "postgres://secondbrain:secondbrain@127.0.0.1:5433/secondbrain"

// App is one isolated server instance bound to its own throwaway database.
type App struct {
	Router http.Handler
	Token  string
	UserID string
	DB     *gorm.DB
	DSN    string
	Config *config.Config
}

// New creates `sb_test_<nanos>_<rand>`, migrates it, seeds the demo user, builds
// the router, and logs in. Everything is dropped when the test ends.
func New(t *testing.T) *App {
	t.Helper()

	return newApp(t, nil)
}

// NewWithConfig is New with a hook to adjust the test configuration, e.g. to
// point DeepSeek at a fake provider.
func NewWithConfig(t *testing.T, configure func(*config.Config)) *App {
	t.Helper()

	return newApp(t, configure)
}

func newApp(t *testing.T, configure func(*config.Config)) *App {
	t.Helper()

	base := dsn()
	name := fmt.Sprintf("sb_test_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	if _, err := admin.ExecContext(context.Background(), "create database "+quoteIdent(name)); err != nil {
		_ = admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "drop database if exists "+quoteIdent(name)+" with (force)")
		_ = admin.Close()
	})

	testDSN, err := replaceDatabase(base, name)
	if err != nil {
		t.Fatalf("build test DSN: %v", err)
	}

	if err := migrate(testDSN); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}

	cfg := &config.Config{
		DatabaseURL:     testDSN,
		BindAddr:        "127.0.0.1:0",
		CORSOrigins:     []string{"http://localhost:5173"},
		TokenTTLDays:    30,
		DeepSeekBaseURL: "https://api.deepseek.com/v1",
		LogLevel:        "error",
	}

	if configure != nil {
		configure(cfg)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := database.Open(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	app := &App{
		Router: bootstrap.NewRouter(cfg, log, bootstrap.Deps{DB: db}),
		DB:     db,
		DSN:    testDSN,
		Config: cfg,
	}

	status, body, err := app.Call(http.MethodPost, "/api/v1/auth/register", "",
		`{"name":"Raka Pratama","email":"raka@example.com","password":"secondbrain"}`)
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("register failed: %d %v", status, body)
	}

	object, _ := body.(map[string]any)
	token, _ := object["token"].(string)
	if token == "" {
		t.Fatalf("register returned no token: %v", body)
	}
	app.Token = token

	if err := db.Raw("select id from users where email = ?", "raka@example.com").Scan(&app.UserID).Error; err != nil {
		t.Fatalf("read registered user id: %v", err)
	}
	if app.UserID == "" {
		t.Fatal("registered user id is empty")
	}

	return app
}

// NewUser inserts another account, for duplicate-email and scoping checks.
func (a *App) NewUser(t *testing.T, email, password string) string {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	id := "u_" + strings.ReplaceAll(strings.Split(email, "@")[0], ".", "_")
	err = a.DB.Exec(
		"insert into users (id, email, password_hash, created_at) values (?, ?, ?, now())",
		id, email, hash,
	).Error
	if err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}

	return id
}

// execute performs one request and returns the recorded response.
func (a *App) execute(method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("content-type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	a.Router.ServeHTTP(recorder, request)

	return recorder
}

// DoWithHeaders performs a request with extra headers and returns the status
// plus the response headers (where the request id round-trip lives).
func (a *App) DoWithHeaders(
	method, path, token, body string,
	headers map[string]string,
) (int, http.Header) {
	recorder := a.execute(method, path, token, body, headers)
	return recorder.Code, recorder.Header()
}

// Call performs one request and returns the status plus the decoded JSON body.
// It never calls t, so goroutine tests can use it directly.
func (a *App) Call(method, path, token, body string) (int, any, error) {
	recorder := a.execute(method, path, token, body, nil)

	status := recorder.Code
	payload := bytes.TrimSpace(recorder.Body.Bytes())
	if len(payload) == 0 {
		return status, nil, nil
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return status, nil, fmt.Errorf("decode response %q: %w", string(payload), err)
	}

	return status, decoded, nil
}

// Do performs a request and returns an object body; a non-object body is a failure.
func (a *App) Do(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()

	status, decoded, err := a.Call(method, path, token, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if decoded == nil {
		return status, nil
	}

	object, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("%s %s: expected an object body, got %T", method, path, decoded)
	}

	return status, object
}

// DoList performs a request and returns an array body.
func (a *App) DoList(t *testing.T, method, path, token, body string) (int, []any) {
	t.Helper()

	status, decoded, err := a.Call(method, path, token, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if decoded == nil {
		return status, nil
	}

	list, ok := decoded.([]any)
	if !ok {
		t.Fatalf("%s %s: expected an array body, got %T", method, path, decoded)
	}

	return status, list
}

// Status performs a request and returns only the status code.
func (a *App) Status(t *testing.T, method, path, token, body string) int {
	t.Helper()

	status, _, err := a.Call(method, path, token, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	return status
}

// Get calls GET with the seeded session token.
func (a *App) Get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodGet, path, a.Token, "")
}

// GetAs calls GET with an explicit token.
func (a *App) GetAs(t *testing.T, path, token string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodGet, path, token, "")
}

// GetList calls GET with the seeded session token and returns an array body.
func (a *App) GetList(t *testing.T, path string) (int, []any) {
	t.Helper()
	return a.DoList(t, http.MethodGet, path, a.Token, "")
}

// Post calls POST with the seeded session token.
func (a *App) Post(t *testing.T, path, body string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodPost, path, a.Token, body)
}

// PostAs calls POST with an explicit token.
func (a *App) PostAs(t *testing.T, path, token, body string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodPost, path, token, body)
}

// Patch calls PATCH with the seeded session token.
func (a *App) Patch(t *testing.T, path, body string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodPatch, path, a.Token, body)
}

// PatchAs calls PATCH with an explicit token.
func (a *App) PatchAs(t *testing.T, path, token, body string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodPatch, path, token, body)
}

// Put calls PUT with the seeded session token and no body.
func (a *App) Put(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	return a.Do(t, http.MethodPut, path, a.Token, "")
}

// Delete calls DELETE with the seeded session token.
func (a *App) Delete(t *testing.T, path string) int {
	t.Helper()
	return a.Status(t, http.MethodDelete, path, a.Token, "")
}

// DeleteAs calls DELETE with an explicit token.
func (a *App) DeleteAs(t *testing.T, path, token string) int {
	t.Helper()
	return a.Status(t, http.MethodDelete, path, token, "")
}

// ErrorMessage reads the `error` field of an error envelope.
func ErrorMessage(body map[string]any) string {
	message, _ := body["error"].(string)
	return message
}

// ErrorCode reads the `code` field of an error envelope.
func ErrorCode(body map[string]any) string {
	code, _ := body["code"].(string)
	return code
}

// StringField reads a string field, failing when it is absent.
func StringField(t *testing.T, body map[string]any, key string) string {
	t.Helper()

	value, ok := body[key].(string)
	if !ok {
		t.Fatalf("field %q is not a string: %v", key, body[key])
	}

	return value
}

func dsn() string {
	for _, key := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	return fallbackDSN
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func replaceDatabase(base, name string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name

	return parsed.String(), nil
}

func migrate(testDSN string) error {
	db, err := sql.Open("pgx", testDSN)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Ping(); err != nil {
		return err
	}

	return migrations.Up(db)
}
