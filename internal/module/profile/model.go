// Package profile owns the single profile row the workspace exposes as `me`.
package profile

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// WireID is the only profile id the API ever exposes; the database keys the
// row by user.
const WireID = "me"

// Profile is the API representation shared by auth, snapshot, and the importer.
type Profile struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Email     string             `json:"email"`
	Role      *string            `json:"role,omitempty"`
	Timezone  *string            `json:"timezone,omitempty"`
	JoinedAt  wiretime.Timestamp `json:"joinedAt"`
	UpdatedAt wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of a profile.
type Row struct {
	UserID    string    `gorm:"column:user_id;primaryKey"`
	Name      string    `gorm:"column:name"`
	Email     string    `gorm:"column:email"`
	Role      *string   `gorm:"column:role"`
	Timezone  *string   `gorm:"column:timezone"`
	JoinedAt  time.Time `gorm:"column:joined_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "profile"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Profile {
	return Profile{
		ID:        WireID,
		Name:      r.Name,
		Email:     r.Email,
		Role:      r.Role,
		Timezone:  r.Timezone,
		JoinedAt:  wiretime.Timestamp(r.JoinedAt),
		UpdatedAt: wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(p Profile) {
	r.Name = p.Name
	r.Email = p.Email
	r.Role = p.Role
	r.Timezone = p.Timezone
	r.JoinedAt = p.JoinedAt.Time()
	r.UpdatedAt = p.UpdatedAt.Time()
}

// Update is the PATCH body: `role` and `timezone` accept an explicit null.
type Update struct {
	Name     *string           `json:"name"`
	Email    *string           `json:"email"`
	Role     httpx.Opt[string] `json:"role"`
	Timezone httpx.Opt[string] `json:"timezone"`
}

// UpdateOptions is the body policy for PATCH /profile.
var UpdateOptions = httpx.Options{Nullable: []string{"role", "timezone"}, Strict: true}
