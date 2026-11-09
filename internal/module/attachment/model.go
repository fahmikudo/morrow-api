// Package attachment stores files for projects, tasks, and knowledge notes.
package attachment

import (
	"time"

	"morrow-api/internal/platform/wiretime"
)

// Entity types accepted by the attachment API.
const (
	EntityProject = "project"
	EntityTask    = "task"
	EntityNote    = "note"
)

// Attachment is the API shape of one uploaded file.
type Attachment struct {
	ID          string             `json:"id"`
	EntityType  string             `json:"entityType"`
	EntityID    string             `json:"entityId"`
	Filename    string             `json:"filename"`
	ContentType string             `json:"contentType"`
	Size        int64              `json:"size"`
	CreatedAt   wiretime.Timestamp `json:"createdAt"`
}

// Row is the persisted attachment metadata.
type Row struct {
	ID          string    `gorm:"column:id;primaryKey"`
	UserID      string    `gorm:"column:user_id"`
	EntityType  string    `gorm:"column:entity_type"`
	EntityID    string    `gorm:"column:entity_id"`
	Filename    string    `gorm:"column:filename"`
	ContentType string    `gorm:"column:content_type"`
	ByteSize    int64     `gorm:"column:byte_size"`
	StorageKey  string    `gorm:"column:storage_key"`
	Status      string    `gorm:"column:status"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "attachments"
}

// ToWire converts a ready row into the API shape.
func (r Row) ToWire() Attachment {
	return Attachment{
		ID:          r.ID,
		EntityType:  r.EntityType,
		EntityID:    r.EntityID,
		Filename:    r.Filename,
		ContentType: r.ContentType,
		Size:        r.ByteSize,
		CreatedAt:   wiretime.Timestamp(r.CreatedAt),
	}
}

// Prepare is the presign request body.
type Prepare struct {
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
}

// UploadTicket is returned before the browser uploads directly to storage.
type UploadTicket struct {
	Attachment Attachment          `json:"attachment"`
	UploadURL  string              `json:"uploadUrl"`
	Method     string              `json:"method"`
	Headers    map[string][]string `json:"headers"`
	ExpiresAt  wiretime.Timestamp  `json:"expiresAt"`
}

// URLResponse is a signed download or preview URL.
type URLResponse struct {
	URL       string             `json:"url"`
	ExpiresAt wiretime.Timestamp `json:"expiresAt"`
}

// Config reports attachment capabilities to the web client.
type Config struct {
	Enabled       bool     `json:"enabled"`
	MaxBytes      int64    `json:"maxBytes"`
	ImageMaxBytes int64    `json:"imageMaxBytes"`
	Extensions    []string `json:"extensions"`
}
