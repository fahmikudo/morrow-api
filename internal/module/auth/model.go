// Package auth owns login, sessions, bearer authentication, and the account
// rows everything else references.
package auth

import (
	"time"

	"morrow-api/internal/module/profile"
)

// LoginRequest is the sign-in body.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest is the sign-up body; the name falls back to the email local
// part when omitted.
type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is the login/register result: the raw session token plus the profile.
type LoginResponse struct {
	Token   string          `json:"token"`
	Profile profile.Profile `json:"profile"`
}

// SessionResponse backs GET /auth/session.
type SessionResponse struct {
	Profile profile.Profile `json:"profile"`
}

// UserRow is the persisted account.
type UserRow struct {
	ID           string    `gorm:"column:id;primaryKey"`
	Email        string    `gorm:"column:email"`
	PasswordHash string    `gorm:"column:password_hash"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName keeps the Gorm mapping explicit.
func (UserRow) TableName() string {
	return "users"
}

// SessionRow is the persisted session.
type SessionRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	TokenHash string    `gorm:"column:token_hash"`
	CreatedAt time.Time `gorm:"column:created_at"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
}

// TableName keeps the Gorm mapping explicit.
func (SessionRow) TableName() string {
	return "sessions"
}

// Credentials is the pair login needs from `users`.
type Credentials struct {
	ID           string
	PasswordHash string
}

// ActiveSession is a session that has not expired.
type ActiveSession struct {
	SessionID string
	UserID    string
}
