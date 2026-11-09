// Package area owns the life areas projects, tasks, notes, and habits reference.
package area

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Area is the API shape of an area.
type Area struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description *string            `json:"description,omitempty"`
	Tone        string             `json:"tone"`
	Icon        string             `json:"icon"`
	CreatedAt   wiretime.Timestamp `json:"createdAt"`
	UpdatedAt   wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of an area.
type Row struct {
	ID          string    `gorm:"column:id;primaryKey"`
	UserID      string    `gorm:"column:user_id"`
	Name        string    `gorm:"column:name"`
	Description *string   `gorm:"column:description"`
	Tone        string    `gorm:"column:tone"`
	Icon        string    `gorm:"column:icon"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "areas"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Area {
	return Area{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Tone:        r.Tone,
		Icon:        r.Icon,
		CreatedAt:   wiretime.Timestamp(r.CreatedAt),
		UpdatedAt:   wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(a Area) {
	r.ID = a.ID
	r.Name = a.Name
	r.Description = a.Description
	r.Tone = a.Tone
	r.Icon = a.Icon
	r.CreatedAt = a.CreatedAt.Time()
	r.UpdatedAt = a.UpdatedAt.Time()
}

// New is the create body.
type New struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Tone        *string `json:"tone"`
	Icon        *string `json:"icon"`
}

// Update is the PATCH body.
type Update struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Tone        *string `json:"tone"`
	Icon        *string `json:"icon"`
}

// UpdateOptions is the body policy for PATCH /areas.
var UpdateOptions = httpx.Options{Strict: true}
