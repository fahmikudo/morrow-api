// Package note owns knowledge-base notes.
package note

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/pgarray"
	"morrow-api/internal/platform/wiretime"
)

// Note is the API shape of a note.
type Note struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Content   string             `json:"content"`
	Tags      []string           `json:"tags"`
	ProjectID *string            `json:"projectId,omitempty"`
	AreaID    *string            `json:"areaId,omitempty"`
	CreatedAt wiretime.Timestamp `json:"createdAt"`
	UpdatedAt wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of a note.
type Row struct {
	ID        string            `gorm:"column:id;primaryKey"`
	UserID    string            `gorm:"column:user_id"`
	Title     string            `gorm:"column:title"`
	Content   string            `gorm:"column:content"`
	Tags      pgarray.TextArray `gorm:"column:tags"`
	ProjectID *string           `gorm:"column:project_id"`
	AreaID    *string           `gorm:"column:area_id"`
	CreatedAt time.Time         `gorm:"column:created_at"`
	UpdatedAt time.Time         `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "notes"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Note {
	return Note{
		ID:        r.ID,
		Title:     r.Title,
		Content:   r.Content,
		Tags:      r.Tags.Strings(),
		ProjectID: r.ProjectID,
		AreaID:    r.AreaID,
		CreatedAt: wiretime.Timestamp(r.CreatedAt),
		UpdatedAt: wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(n Note) {
	r.ID = n.ID
	r.Title = n.Title
	r.Content = n.Content
	r.Tags = pgarray.Texts(n.Tags)
	r.ProjectID = n.ProjectID
	r.AreaID = n.AreaID
	r.CreatedAt = n.CreatedAt.Time()
	r.UpdatedAt = n.UpdatedAt.Time()
}

// New is the create body.
type New struct {
	Title     string   `json:"title"`
	Content   *string  `json:"content"`
	Tags      []string `json:"tags"`
	ProjectID *string  `json:"projectId"`
	AreaID    *string  `json:"areaId"`
}

// Update is the PATCH body; `projectId` and `areaId` accept an explicit null.
type Update struct {
	Title     *string           `json:"title"`
	Content   *string           `json:"content"`
	Tags      []string          `json:"tags"`
	ProjectID httpx.Opt[string] `json:"projectId"`
	AreaID    httpx.Opt[string] `json:"areaId"`
}

// UpdateOptions is the body policy for PATCH /notes.
var UpdateOptions = httpx.Options{Nullable: []string{"projectId", "areaId"}, Strict: true}
