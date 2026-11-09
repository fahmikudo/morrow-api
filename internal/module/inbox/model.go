// Package inbox owns the quick-capture inbox.
package inbox

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Item is the API shape of an inbox item.
type Item struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Content    *string             `json:"content,omitempty"`
	Type       string              `json:"type"`
	CreatedAt  wiretime.Timestamp  `json:"createdAt"`
	ArchivedAt *wiretime.Timestamp `json:"archivedAt,omitempty"`
}

// Row is the persisted shape of an inbox item.
type Row struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Title      string     `gorm:"column:title"`
	Content    *string    `gorm:"column:content"`
	Type       string     `gorm:"column:type"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	ArchivedAt *time.Time `gorm:"column:archived_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "inbox_items"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Item {
	var archivedAt *wiretime.Timestamp
	if r.ArchivedAt != nil {
		value := wiretime.Timestamp(*r.ArchivedAt)
		archivedAt = &value
	}

	return Item{
		ID:         r.ID,
		Title:      r.Title,
		Content:    r.Content,
		Type:       r.Type,
		CreatedAt:  wiretime.Timestamp(r.CreatedAt),
		ArchivedAt: archivedAt,
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(item Item) {
	r.ID = item.ID
	r.Title = item.Title
	r.Content = item.Content
	r.Type = item.Type
	r.CreatedAt = item.CreatedAt.Time()
	if item.ArchivedAt != nil {
		value := item.ArchivedAt.Time()
		r.ArchivedAt = &value
	} else {
		r.ArchivedAt = nil
	}
}

// New is the create body.
type New struct {
	Title   string  `json:"title"`
	Content *string `json:"content"`
	Type    string  `json:"type"`
}

// Update is the PATCH body.
type Update struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Type    *string `json:"type"`
}

// UpdateOptions is the body policy for PATCH /inbox/{id}.
var UpdateOptions = httpx.Options{Strict: true}
