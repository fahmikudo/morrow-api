// Package calendar owns the Google Calendar OAuth connection and event sync.
package calendar

import (
	"time"
)

// IntegrationID is the settings row this module owns.
const IntegrationID = "google-calendar"

// DefaultCalendarID imports and writes the account's primary calendar.
const DefaultCalendarID = "primary"

// CredentialRow is the encrypted OAuth credential.
type CredentialRow struct {
	UserID          string     `gorm:"column:user_id;primaryKey"`
	IntegrationID   string     `gorm:"column:integration_id;primaryKey"`
	AccessTokenEnc  string     `gorm:"column:access_token_enc"`
	RefreshTokenEnc *string    `gorm:"column:refresh_token_enc"`
	TokenExpiry     *time.Time `gorm:"column:token_expiry"`
	Scope           *string    `gorm:"column:scope"`
	SyncToken       *string    `gorm:"column:sync_token"`
	LastSyncAt      *time.Time `gorm:"column:last_sync_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (CredentialRow) TableName() string {
	return "integration_credentials"
}

// OAuthStateRow is a one-time OAuth state with its PKCE verifier.
type OAuthStateRow struct {
	StateHash       string    `gorm:"column:state_hash;primaryKey"`
	UserID          string    `gorm:"column:user_id"`
	CodeVerifierEnc string    `gorm:"column:code_verifier_enc"`
	ExpiresAt       time.Time `gorm:"column:expires_at"`
}

// TableName keeps the Gorm mapping explicit.
func (OAuthStateRow) TableName() string {
	return "oauth_states"
}

// LinkRow maps a local event to a Google event. EventID is null for a pending
// deletion (the local event is already gone).
type LinkRow struct {
	ID                string     `gorm:"column:id;primaryKey"`
	EventID           *string    `gorm:"column:event_id"`
	UserID            string     `gorm:"column:user_id"`
	IntegrationID     string     `gorm:"column:integration_id"`
	CalendarID        string     `gorm:"column:calendar_id"`
	ExternalID        string     `gorm:"column:external_id"`
	ExternalETag      *string    `gorm:"column:external_etag"`
	ExternalUpdatedAt *time.Time `gorm:"column:external_updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (LinkRow) TableName() string {
	return "calendar_event_links"
}

// CalendarItem is the compact calendar-list shape the picker needs.
type CalendarItem struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	Primary    bool   `json:"primary"`
	AccessRole string `json:"accessRole"`
}

// SelectRequest is the PUT /integrations/google-calendar/calendar body.
type SelectRequest struct {
	CalendarID string `json:"calendarId"`
}
