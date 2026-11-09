// Package pepy implements the SoftwareSeni Pepy task provider.
package pepy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"morrow-api/internal/config"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/task"
	"morrow-api/internal/module/tasksync"
	"morrow-api/internal/platform/features"
	"morrow-api/internal/platform/secretbox"
	"morrow-api/internal/platform/timezone"
	"morrow-api/internal/platform/wiretime"

	"log/slog"
	"sync"
)

// ErrNotConnected means the user has not connected Pepy yet.
var ErrNotConnected = errors.New("pepy: not connected")

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("pepy: HTTP %d: %s", e.status, e.message)
}

func unauthorized(err error) bool {
	var status *statusError
	return errors.As(err, &status) && status.status == http.StatusUnauthorized
}

// Provider is the Pepy task provider.
type Provider struct {
	cfg      *config.Config
	client   *http.Client
	repo     *tasksync.Repository
	profiles *profile.Repository
	box      *secretbox.Box
	log      *slog.Logger
	locks    sync.Map
}

// NewProvider wires the Pepy provider.
func NewProvider(
	cfg *config.Config,
	client *http.Client,
	repo *tasksync.Repository,
	profiles *profile.Repository,
	box *secretbox.Box,
	log *slog.Logger,
) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Provider{
		cfg: cfg, client: client, repo: repo, profiles: profiles, box: box, log: log,
	}
}

// ID identifies the settings integration.
func (p *Provider) ID() string {
	return tasksync.PepyIntegrationID
}

// EnabledFor reports whether the feature flag and allowlist include this user.
func (p *Provider) EnabledFor(ctx context.Context, userID string) bool {
	current, err := p.profiles.Get(ctx, userID)
	if err != nil || current == nil {
		return false
	}

	return features.PepyEnabled(p.cfg, current.Email)
}

// Connected reports whether a token exists.
func (p *Provider) Connected(ctx context.Context, userID string) bool {
	credential, err := p.repo.Credential(ctx, userID, tasksync.PepyIntegrationID)
	return err == nil && credential != nil
}

// Connect logs in and returns the Pepy account identity.
func (p *Provider) Connect(ctx context.Context, userID string) (tasksync.Account, error) {
	if !p.EnabledFor(ctx, userID) {
		return tasksync.Account{}, tasksync.ErrUnavailable
	}

	return p.login(ctx, userID)
}

// EnsureToken refreshes the token when it is old or near expiry.
func (p *Provider) EnsureToken(ctx context.Context, userID string) error {
	if !p.EnabledFor(ctx, userID) {
		return tasksync.ErrUnavailable
	}
	_, err := p.token(ctx, userID, false)
	return err
}

func (p *Provider) token(ctx context.Context, userID string, force bool) (string, error) {
	credential, err := p.repo.Credential(ctx, userID, tasksync.PepyIntegrationID)
	if err != nil {
		return "", err
	}
	if credential == nil {
		return "", ErrNotConnected
	}

	if !force && p.valid(credential) {
		return p.box.Open(credential.AccessTokenEnc)
	}

	unlock := p.lock(userID)
	defer unlock()

	credential, err = p.repo.Credential(ctx, userID, tasksync.PepyIntegrationID)
	if err != nil {
		return "", err
	}
	if credential == nil {
		return "", ErrNotConnected
	}
	if !force && p.valid(credential) {
		return p.box.Open(credential.AccessTokenEnc)
	}

	if _, err := p.loginLocked(ctx, userID, credential); err != nil {
		return "", err
	}

	credential, err = p.repo.Credential(ctx, userID, tasksync.PepyIntegrationID)
	if err != nil || credential == nil {
		return "", ErrNotConnected
	}
	return p.box.Open(credential.AccessTokenEnc)
}

func (p *Provider) valid(credential *tasksync.CredentialRow) bool {
	if credential.TokenExpiry == nil || !credential.TokenExpiry.After(time.Now().Add(5*time.Minute)) {
		return false
	}
	interval := time.Duration(p.cfg.PepyLoginIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 72 * time.Hour
	}

	return credential.UpdatedAt.After(time.Now().Add(-interval))
}

func (p *Provider) login(ctx context.Context, userID string) (tasksync.Account, error) {
	unlock := p.lock(userID)
	defer unlock()

	credential, err := p.repo.Credential(ctx, userID, tasksync.PepyIntegrationID)
	if err != nil {
		return tasksync.Account{}, err
	}

	return p.loginLocked(ctx, userID, credential)
}

func (p *Provider) loginLocked(
	ctx context.Context,
	userID string,
	existing *tasksync.CredentialRow,
) (tasksync.Account, error) {
	body, err := json.Marshal(map[string]string{
		"username": p.cfg.PepyUsername,
		"password": p.cfg.PepyPassword,
	})
	if err != nil {
		return tasksync.Account{}, err
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.cfg.PepyBaseURL+"/users/signin/sshub", bytes.NewReader(body),
	)
	if err != nil {
		return tasksync.Account{}, err
	}
	request.Header.Set("content-type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return tasksync.Account{}, err
	}
	defer closeBody(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tasksync.Account{}, statusErrorFrom(response)
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return tasksync.Account{}, err
	}
	if payload.Data.Token == "" {
		return tasksync.Account{}, errors.New("pepy: login returned no token")
	}

	tokenEnc, err := p.box.Seal(payload.Data.Token)
	if err != nil {
		return tasksync.Account{}, err
	}

	now := wiretime.Now().Time()
	expiry := jwtExpiry(payload.Data.Token, now.Add(72*time.Hour))
	row := tasksync.CredentialRow{
		UserID:         userID,
		IntegrationID:  tasksync.PepyIntegrationID,
		AccessTokenEnc: tokenEnc,
		TokenExpiry:    &expiry,
		UpdatedAt:      now,
	}
	if existing != nil {
		row.LastSyncAt = existing.LastSyncAt
	}
	if err := p.repo.SaveCredential(ctx, row); err != nil {
		return tasksync.Account{}, err
	}

	return tasksync.Account{Email: payload.Data.Email, Name: payload.Data.Name}, nil
}

func (p *Provider) lock(userID string) func() {
	value, _ := p.locks.LoadOrStore(userID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

// Create creates the Pepy task and returns its remote id.
func (p *Provider) Create(
	ctx context.Context,
	userID string,
	current task.Task,
	uuid string,
) (string, error) {
	payload, err := p.payload(ctx, userID, current, uuid)
	if err != nil {
		return "", err
	}
	return p.call(ctx, userID, func(token string) (string, error) {
		var response struct {
			Data struct {
				ID json.Number `json:"id"`
			} `json:"data"`
		}
		if err := p.send(ctx, http.MethodPost, token, "/tasks", payload, &response); err != nil {
			return "", err
		}
		return response.Data.ID.String(), nil
	})
}

// Update updates the linked Pepy task.
func (p *Provider) Update(
	ctx context.Context,
	userID string,
	current task.Task,
	link tasksync.LinkRow,
) error {
	if link.ExternalID == nil {
		return errors.New("pepy: link has no external id")
	}
	ref := ""
	if link.ExternalRef != nil {
		ref = *link.ExternalRef
	}
	payload, err := p.payload(ctx, userID, current, ref)
	if err != nil {
		return err
	}
	_, err = p.call(ctx, userID, func(token string) (string, error) {
		return "", p.send(ctx, http.MethodPut, token, "/tasks/"+*link.ExternalID, payload, nil)
	})
	return err
}

// Delete removes the linked Pepy task.
func (p *Provider) Delete(ctx context.Context, userID string, link tasksync.LinkRow) error {
	if link.ExternalID == nil {
		return nil
	}
	_, err := p.call(ctx, userID, func(token string) (string, error) {
		return "", p.delete(ctx, token, "/tasks/"+*link.ExternalID)
	})
	return err
}

func (p *Provider) call(
	ctx context.Context,
	userID string,
	run func(token string) (string, error),
) (string, error) {
	token, err := p.token(ctx, userID, false)
	if err != nil {
		return "", err
	}

	result, err := run(token)
	if !unauthorized(err) {
		return result, err
	}

	token, err = p.token(ctx, userID, true)
	if err != nil {
		return "", err
	}

	return run(token)
}

func (p *Provider) send(
	ctx context.Context,
	method, token, path string,
	payload any,
	dst any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx, method, p.cfg.PepyBaseURL+path, bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+token)
	request.Header.Set("content-type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusErrorFrom(response)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(dst)
}

func (p *Provider) delete(ctx context.Context, token, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.cfg.PepyBaseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+token)

	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusErrorFrom(response)
	}

	return nil
}

func (p *Provider) payload(
	ctx context.Context,
	userID string,
	current task.Task,
	uuid string,
) (map[string]any, error) {
	if current.DueDate == nil || current.StartTime == nil || current.EndTime == nil {
		return nil, errors.New("pepy: task needs a due date and start/end time")
	}

	var profileTimezone *string
	if profileValue, err := p.profiles.Get(ctx, userID); err == nil && profileValue != nil {
		profileTimezone = profileValue.Timezone
	}
	location := timezone.Resolve(profileTimezone, p.cfg.AppTimezone)

	date := current.DueDate.String()
	start, startErr := time.ParseInLocation("2006-01-02 15:04", date+" "+*current.StartTime, location)
	end, endErr := time.ParseInLocation("2006-01-02 15:04", date+" "+*current.EndTime, location)
	if startErr != nil || endErr != nil {
		return nil, errors.New("pepy: invalid task time box")
	}

	scheduledStart := start.UTC().Format("2006-01-02T15:04:05.000Z")
	scheduledStop := end.UTC().Format("2006-01-02T15:04:05.000Z")
	payload := map[string]any{
		"auto_expand":             false,
		"project_id":              p.cfg.PepyProjectID,
		"scheduled_start_time":    scheduledStart,
		"scheduled_stop_time":     scheduledStop,
		"start_time":              scheduledStart,
		"user_id":                 p.cfg.PepyUserID,
		"is_overtime":             false,
		"is_billable":             true,
		"external_task_reference": current.Key,
		"title":                   current.Title,
		"uuid":                    uuid,
	}
	if current.Description != nil {
		payload["description"] = *current.Description
	}
	if current.Status == "done" {
		payload["stop_time"] = scheduledStop
	}

	return payload, nil
}

func statusErrorFrom(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	return &statusError{status: response.StatusCode, message: message}
}

func closeBody(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

func jwtExpiry(token string, fallback time.Time) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return fallback
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Exp <= 0 {
		return fallback
	}

	return time.Unix(claims.Exp, 0).UTC()
}
