// Package apitoken issues the `sb_…` personal access tokens used by scripts and
// MCP servers. Only the SHA-256 hash is stored.
package apitoken

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// APIToken is the metadata a client can read back; the token itself never
// appears again after creation.
type APIToken struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	CreatedAt  wiretime.Timestamp  `json:"createdAt"`
	LastUsedAt *wiretime.Timestamp `json:"lastUsedAt,omitempty"`
}

// CreateResponse is the single chance to read the plaintext token.
type CreateResponse struct {
	Token    string   `json:"token"`
	APIToken APIToken `json:"apiToken"`
}

// CreateRequest is the create body; unknown keys are rejected.
type CreateRequest struct {
	Name string `json:"name"`
}

// CreateOptions is the body policy for POST /api-tokens.
var CreateOptions = httpx.Options{Strict: true}

// Row is the persisted token: hash only, never the plaintext.
type Row struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Name       string     `gorm:"column:name"`
	TokenHash  string     `gorm:"column:token_hash"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "api_tokens"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() APIToken {
	var lastUsedAt *wiretime.Timestamp
	if r.LastUsedAt != nil {
		value := wiretime.Timestamp(*r.LastUsedAt)
		lastUsedAt = &value
	}

	return APIToken{
		ID:         r.ID,
		Name:       r.Name,
		CreatedAt:  wiretime.Timestamp(r.CreatedAt),
		LastUsedAt: lastUsedAt,
	}
}
