// Package settings owns the integration toggles and the DeepSeek settings.
package settings

import (
	"strings"
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// IntegrationIDs is the fixed catalogue; order only matters for seeding.
var IntegrationIDs = []string{
	"google-calendar",
	"softwareseni-pepy",
}

// Models is the fallback catalogue used when the provider cannot be reached.
var Models = []string{"deepseek-flash", "deepseek-v4-pro"}

// LegacyModels are still accepted so existing rows keep working; the provider
// serves them as aliases of the current models.
var LegacyModels = []string{"deepseek-chat", "deepseek-reasoner"}

// EffortLevels are the reasoning-effort values the provider documents.
var EffortLevels = []string{"none", "low", "high", "max"}

// AIModel is one model the provider offers.
type AIModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextWindow int64    `json:"contextWindow"`
	EffortLevels  []string `json:"effortLevels"`
}

// IntegrationConnection is the API shape of one integration.
type IntegrationConnection struct {
	Enabled      bool                `json:"enabled"`
	Connected    bool                `json:"connected"`
	LastSyncAt   *wiretime.Timestamp `json:"lastSyncAt,omitempty"`
	AccountEmail *string             `json:"accountEmail,omitempty"`
	CalendarID   *string             `json:"calendarId,omitempty"`
	LastError    *string             `json:"lastError,omitempty"`
}

// AISettings is the API shape of the assistant settings.
type AISettings struct {
	Enabled       bool                `json:"enabled"`
	Provider      string              `json:"provider"`
	Model         string              `json:"model"`
	Status        string              `json:"status"`
	LastTestedAt  *wiretime.Timestamp `json:"lastTestedAt,omitempty"`
	KeyConfigured bool                `json:"keyConfigured"`
}

// TestResult is the body POST /ai/test returns.
type TestResult struct {
	OK       bool               `json:"ok"`
	Status   string             `json:"status"`
	Message  string             `json:"message"`
	TestedAt wiretime.Timestamp `json:"testedAt"`
}

// Response is the body GET /settings returns.
type Response struct {
	Integrations map[string]IntegrationConnection `json:"integrations"`
	AI           AISettings                       `json:"ai"`
	// AIModels is the provider catalogue (fallback list when it cannot be read).
	AIModels []AIModel `json:"aiModels"`
	// AIEffort is the stored reasoning effort, null when the provider default applies.
	AIEffort *string `json:"aiEffort"`
	// GoogleCalendarConfigured reports whether OAuth + encryption are available.
	GoogleCalendarConfigured bool `json:"googleCalendarConfigured"`
	// Features gates optional integrations per account.
	Features map[string]bool `json:"features"`
}

// UpdateAI is the PATCH /ai body; `apiKey` and `reasoningEffort` accept an
// explicit null. The key is write-only: responses only expose keyConfigured.
type UpdateAI struct {
	Enabled         *bool             `json:"enabled"`
	Model           *string           `json:"model"`
	APIKey          httpx.Opt[string] `json:"apiKey"`
	ReasoningEffort httpx.Opt[string] `json:"reasoningEffort"`
}

// UpdateAIOptions is the body policy for PATCH /ai.
var UpdateAIOptions = httpx.Options{
	Nullable: []string{"apiKey", "reasoningEffort"},
	Strict:   true,
}

// IntegrationPatch is the PATCH /integrations/{id} body.
type IntegrationPatch struct {
	Enabled bool `json:"enabled"`
}

// IntegrationPatchOptions is the body policy for PATCH /integrations/{id}.
var IntegrationPatchOptions = httpx.Options{Strict: true}

// IntegrationRow is the persisted shape of one integration toggle.
type IntegrationRow struct {
	UserID        string     `gorm:"column:user_id;primaryKey"`
	IntegrationID string     `gorm:"column:integration_id;primaryKey"`
	Enabled       bool       `gorm:"column:enabled"`
	Connected     bool       `gorm:"column:connected"`
	LastSyncAt    *time.Time `gorm:"column:last_sync_at"`
	AccountEmail  *string    `gorm:"column:account_email"`
	CalendarID    *string    `gorm:"column:calendar_id"`
	LastError     *string    `gorm:"column:last_error"`
}

// TableName keeps the Gorm mapping explicit.
func (IntegrationRow) TableName() string {
	return "integrations"
}

// ToWire converts a stored row into the API shape.
func (r IntegrationRow) ToWire() IntegrationConnection {
	var lastSyncAt *wiretime.Timestamp
	if r.LastSyncAt != nil {
		value := wiretime.Timestamp(*r.LastSyncAt)
		lastSyncAt = &value
	}

	return IntegrationConnection{
		Enabled:      r.Enabled,
		Connected:    r.Connected,
		LastSyncAt:   lastSyncAt,
		AccountEmail: r.AccountEmail,
		CalendarID:   r.CalendarID,
		LastError:    r.LastError,
	}
}

// AIRow is the persisted shape of the assistant settings.
type AIRow struct {
	UserID          string     `gorm:"column:user_id;primaryKey"`
	Enabled         bool       `gorm:"column:enabled"`
	Provider        string     `gorm:"column:provider"`
	Model           string     `gorm:"column:model"`
	ReasoningEffort *string    `gorm:"column:reasoning_effort"`
	APIKey          *string    `gorm:"column:api_key" json:"-"`
	Status          string     `gorm:"column:status"`
	LastTestedAt    *time.Time `gorm:"column:last_tested_at"`
}

// APIKeyValue returns the stored key, trimmed, or the empty string.
func (r AIRow) APIKeyValue() string {
	if r.APIKey == nil {
		return ""
	}

	return strings.TrimSpace(*r.APIKey)
}

// KeyConfigured reports whether this user stored a DeepSeek key.
func (r AIRow) KeyConfigured() bool {
	return r.APIKeyValue() != ""
}

// TableName keeps the Gorm mapping explicit.
func (AIRow) TableName() string {
	return "ai_settings"
}

// ToWire converts the stored settings into the API shape; the key itself is
// never serialized.
func (r AIRow) ToWire() AISettings {
	var lastTestedAt *wiretime.Timestamp
	if r.LastTestedAt != nil {
		value := wiretime.Timestamp(*r.LastTestedAt)
		lastTestedAt = &value
	}

	return AISettings{
		Enabled:       r.Enabled,
		Provider:      r.Provider,
		Model:         r.Model,
		Status:        r.Status,
		LastTestedAt:  lastTestedAt,
		KeyConfigured: r.KeyConfigured(),
	}
}
