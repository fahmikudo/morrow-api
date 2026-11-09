// Package task owns tasks, their Jira-lite keys, and the per-project counters.
package task

import (
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/pgarray"
	"morrow-api/internal/platform/wiretime"
)

// Task is the API shape of a task.
type Task struct {
	ID          string         `json:"id"`
	Key         string         `json:"key"`
	Title       string         `json:"title"`
	Description *string        `json:"description,omitempty"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Priority    string         `json:"priority"`
	DueDate     *wiretime.Date `json:"dueDate,omitempty"`
	StartTime   *string        `json:"startTime,omitempty"`
	EndTime     *string        `json:"endTime,omitempty"`
	ProjectID   *string        `json:"projectId,omitempty"`
	AreaID      *string        `json:"areaId,omitempty"`
	ParentID    *string        `json:"parentId,omitempty"`
	Labels      []string       `json:"labels"`
	// SyncToPepy is computed from task_external_links, not a task column.
	SyncToPepy bool               `json:"syncToPepy,omitempty"`
	CreatedAt  wiretime.Timestamp `json:"createdAt"`
	UpdatedAt  wiretime.Timestamp `json:"updatedAt"`
}

// Row is the persisted shape of a task.
type Row struct {
	ID          string            `gorm:"column:id;primaryKey"`
	UserID      string            `gorm:"column:user_id"`
	Key         string            `gorm:"column:key"`
	Title       string            `gorm:"column:title"`
	Description *string           `gorm:"column:description"`
	Type        string            `gorm:"column:type"`
	Status      string            `gorm:"column:status"`
	Priority    string            `gorm:"column:priority"`
	DueDate     *time.Time        `gorm:"column:due_date"`
	StartTime   *wiretime.Clock   `gorm:"column:start_time"`
	EndTime     *wiretime.Clock   `gorm:"column:end_time"`
	ProjectID   *string           `gorm:"column:project_id"`
	AreaID      *string           `gorm:"column:area_id"`
	ParentID    *string           `gorm:"column:parent_id"`
	Labels      pgarray.TextArray `gorm:"column:labels"`
	CreatedAt   time.Time         `gorm:"column:created_at"`
	UpdatedAt   time.Time         `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "tasks"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Task {
	var due *wiretime.Date
	if r.DueDate != nil {
		value := wiretime.Date(*r.DueDate)
		due = &value
	}

	return Task{
		ID:          r.ID,
		Key:         r.Key,
		Title:       r.Title,
		Description: r.Description,
		Type:        r.Type,
		Status:      r.Status,
		Priority:    r.Priority,
		DueDate:     due,
		StartTime:   wiretime.ClockText(r.StartTime),
		EndTime:     wiretime.ClockText(r.EndTime),
		ProjectID:   r.ProjectID,
		AreaID:      r.AreaID,
		ParentID:    r.ParentID,
		Labels:      r.Labels.Strings(),
		CreatedAt:   wiretime.Timestamp(r.CreatedAt),
		UpdatedAt:   wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(t Task) {
	r.ID = t.ID
	r.Key = t.Key
	r.Title = t.Title
	r.Description = t.Description
	r.Type = t.Type
	r.Status = t.Status
	r.Priority = t.Priority
	if t.DueDate != nil {
		value := t.DueDate.Time()
		r.DueDate = &value
	} else {
		r.DueDate = nil
	}
	r.StartTime = wiretime.ClockValue(t.StartTime)
	r.EndTime = wiretime.ClockValue(t.EndTime)
	r.ProjectID = t.ProjectID
	r.AreaID = t.AreaID
	r.ParentID = t.ParentID
	r.Labels = pgarray.Texts(t.Labels)
	r.CreatedAt = t.CreatedAt.Time()
	r.UpdatedAt = t.UpdatedAt.Time()
}

// New is the create body.
type New struct {
	Title       string   `json:"title"`
	Description *string  `json:"description"`
	Type        *string  `json:"type"`
	Status      *string  `json:"status"`
	Priority    *string  `json:"priority"`
	DueDate     *string  `json:"dueDate"`
	StartTime   *string  `json:"startTime"`
	EndTime     *string  `json:"endTime"`
	ProjectID   *string  `json:"projectId"`
	AreaID      *string  `json:"areaId"`
	ParentID    *string  `json:"parentId"`
	Labels      []string `json:"labels"`
	SyncToPepy  *bool    `json:"syncToPepy,omitempty"`
}

// Update is the PATCH body; four fields accept an explicit null.
type Update struct {
	Title       *string           `json:"title"`
	Description *string           `json:"description"`
	Type        *string           `json:"type"`
	Status      *string           `json:"status"`
	Priority    *string           `json:"priority"`
	Labels      []string          `json:"labels"`
	SyncToPepy  *bool             `json:"syncToPepy,omitempty"`
	DueDate     httpx.Opt[string] `json:"dueDate"`
	StartTime   httpx.Opt[string] `json:"startTime"`
	EndTime     httpx.Opt[string] `json:"endTime"`
	ProjectID   httpx.Opt[string] `json:"projectId"`
	AreaID      httpx.Opt[string] `json:"areaId"`
	ParentID    httpx.Opt[string] `json:"parentId"`
}

// UpdateOptions is the body policy for PATCH /tasks.
var UpdateOptions = httpx.Options{
	Nullable: []string{"dueDate", "startTime", "endTime", "projectId", "areaId", "parentId"},
	Strict:   true,
}
