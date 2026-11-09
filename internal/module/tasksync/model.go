// Package tasksync keeps task-to-provider links generic. Pepy is the first
// provider; Jira/Linear can be added without another task column.
package tasksync

import (
	"context"
	"time"

	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/features"
)

// PepyIntegrationID is the settings key for SoftwareSeni Pepy.
const PepyIntegrationID = features.PepyIntegrationID

// LinkRow maps one task to one external task. TaskID is null for a pending
// remote deletion.
type LinkRow struct {
	ID            string     `gorm:"column:id;primaryKey"`
	UserID        string     `gorm:"column:user_id"`
	TaskID        *string    `gorm:"column:task_id"`
	IntegrationID string     `gorm:"column:integration_id"`
	ExternalID    *string    `gorm:"column:external_id"`
	ExternalRef   *string    `gorm:"column:external_ref"`
	Enabled       bool       `gorm:"column:enabled"`
	LastSyncedAt  *time.Time `gorm:"column:last_synced_at"`
	LastError     *string    `gorm:"column:last_error"`
}

// TableName keeps the Gorm mapping explicit.
func (LinkRow) TableName() string {
	return "task_external_links"
}

// CredentialRow is a provider bearer token stored encrypted.
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

// Account is the provider identity shown after login.
type Account struct {
	Email string
	Name  string
}

// Provider is the remote side of task sync.
type Provider interface {
	ID() string
	EnabledFor(ctx context.Context, userID string) bool
	Connected(ctx context.Context, userID string) bool
	Connect(ctx context.Context, userID string) (Account, error)
	EnsureToken(ctx context.Context, userID string) error
	Create(ctx context.Context, userID string, current task.Task, uuid string) (string, error)
	Update(ctx context.Context, userID string, current task.Task, link LinkRow) error
	Delete(ctx context.Context, userID string, link LinkRow) error
}
