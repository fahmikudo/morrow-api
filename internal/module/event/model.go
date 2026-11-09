// Package event owns the month-calendar entries.
package event

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Event is the API shape of a calendar event.
type Event struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Date      wiretime.Date      `json:"date"`
	StartTime *string            `json:"startTime,omitempty"`
	EndTime   *string            `json:"endTime,omitempty"`
	Notes     *string            `json:"notes,omitempty"`
	ProjectID *string            `json:"projectId,omitempty"`
	Source    string             `json:"source"`
	CreatedAt wiretime.Timestamp `json:"createdAt"`
	UpdatedAt wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of an event.
type Row struct {
	ID        string          `gorm:"column:id;primaryKey"`
	UserID    string          `gorm:"column:user_id"`
	Title     string          `gorm:"column:title"`
	Date      time.Time       `gorm:"column:date"`
	StartTime *wiretime.Clock `gorm:"column:start_time"`
	EndTime   *wiretime.Clock `gorm:"column:end_time"`
	Notes     *string         `gorm:"column:notes"`
	ProjectID *string         `gorm:"column:project_id"`
	Source    string          `gorm:"column:source"`
	CreatedAt time.Time       `gorm:"column:created_at"`
	UpdatedAt time.Time       `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "events"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Event {
	return Event{
		ID:        r.ID,
		Title:     r.Title,
		Date:      wiretime.Date(r.Date),
		StartTime: wiretime.ClockText(r.StartTime),
		EndTime:   wiretime.ClockText(r.EndTime),
		Notes:     r.Notes,
		ProjectID: r.ProjectID,
		Source:    r.Source,
		CreatedAt: wiretime.Timestamp(r.CreatedAt),
		UpdatedAt: wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(e Event) {
	r.ID = e.ID
	r.Title = e.Title
	r.Date = e.Date.Time()
	r.StartTime = wiretime.ClockValue(e.StartTime)
	r.EndTime = wiretime.ClockValue(e.EndTime)
	r.Notes = e.Notes
	r.ProjectID = e.ProjectID
	r.Source = e.Source
	if r.Source == "" {
		r.Source = "local"
	}
	r.CreatedAt = e.CreatedAt.Time()
	r.UpdatedAt = e.UpdatedAt.Time()
}

// New is the create body; `date` is required.
type New struct {
	Title     string  `json:"title"`
	Date      string  `json:"date"`
	StartTime *string `json:"startTime"`
	EndTime   *string `json:"endTime"`
	Notes     *string `json:"notes"`
	ProjectID *string `json:"projectId"`
}

// Update is the PATCH body; three fields accept an explicit null.
type Update struct {
	Title     *string           `json:"title"`
	Date      *string           `json:"date"`
	Notes     *string           `json:"notes"`
	StartTime httpx.Opt[string] `json:"startTime"`
	EndTime   httpx.Opt[string] `json:"endTime"`
	ProjectID httpx.Opt[string] `json:"projectId"`
}

// UpdateOptions is the body policy for PATCH /events.
var UpdateOptions = httpx.Options{
	Nullable: []string{"startTime", "endTime", "projectId"},
	Strict:   true,
}
