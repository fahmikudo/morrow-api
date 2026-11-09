// Package project owns projects and the immutable key allocated for each one.
package project

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Project is the API shape of a project.
type Project struct {
	ID          string             `json:"id"`
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Description *string            `json:"description,omitempty"`
	Status      string             `json:"status"`
	AreaID      *string            `json:"areaId,omitempty"`
	CreatedAt   wiretime.Timestamp `json:"createdAt"`
	UpdatedAt   wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of a project.
type Row struct {
	ID          string    `gorm:"column:id;primaryKey"`
	UserID      string    `gorm:"column:user_id"`
	Key         string    `gorm:"column:key"`
	Name        string    `gorm:"column:name"`
	Description *string   `gorm:"column:description"`
	Status      string    `gorm:"column:status"`
	AreaID      *string   `gorm:"column:area_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "projects"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Project {
	return Project{
		ID:          r.ID,
		Key:         r.Key,
		Name:        r.Name,
		Description: r.Description,
		Status:      r.Status,
		AreaID:      r.AreaID,
		CreatedAt:   wiretime.Timestamp(r.CreatedAt),
		UpdatedAt:   wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape, keeping the original key.
func (r *Row) Load(p Project) {
	r.ID = p.ID
	r.Key = p.Key
	r.Name = p.Name
	r.Description = p.Description
	r.Status = p.Status
	r.AreaID = p.AreaID
	r.CreatedAt = p.CreatedAt.Time()
	r.UpdatedAt = p.UpdatedAt.Time()
}

// New is the create body.
type New struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	AreaID      *string `json:"areaId"`
}

// Update is the PATCH body; `areaId` accepts an explicit null.
type Update struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Status      *string           `json:"status"`
	AreaID      httpx.Opt[string] `json:"areaId"`
}

// UpdateOptions is the body policy for PATCH /projects.
var UpdateOptions = httpx.Options{Nullable: []string{"areaId"}, Strict: true}
