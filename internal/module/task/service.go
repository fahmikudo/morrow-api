package task

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/project"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// keyAttempts bounds the allocate/retry loop on concurrent creates.
const keyAttempts = 10

// TaskSync mirrors opted-in tasks to external providers.
type TaskSync interface {
	Available(ctx context.Context, userID string) bool
	EnabledTaskIDs(ctx context.Context, userID string) (map[string]bool, error)
	SetEnabled(ctx context.Context, userID, taskID string, enabled bool) error
	Push(ctx context.Context, userID string, current Task) error
	Delete(ctx context.Context, userID, taskID string) error
}

// Service applies the task rules inside one transaction per write.
type Service struct {
	db          *gorm.DB
	log         *slog.Logger
	repo        *Repository
	projects    *project.Repository
	areas       *area.Repository
	counters    *Counters
	taskSync    TaskSync
	attachments AttachmentCleaner
}

// AttachmentCleaner removes files attached to a deleted entity.
type AttachmentCleaner interface {
	DeleteForEntity(ctx context.Context, userID, entityType, entityID string)
}

// NewService wires the task service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	projects *project.Repository,
	areas *area.Repository,
	counters *Counters,
	taskSync TaskSync,
	attachments AttachmentCleaner,
) *Service {
	return &Service{
		db: db, log: log, repo: repo, projects: projects,
		areas: areas, counters: counters, taskSync: taskSync, attachments: attachments,
	}
}

// List returns every task.
func (s *Service) List(ctx context.Context, userID string) ([]Task, error) {
	tasks, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	s.attachSync(ctx, userID, tasks)
	return tasks, nil
}

// Get returns one task or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Task, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Task not found")
	}

	tasks := []Task{*current}
	s.attachSync(ctx, userID, tasks)
	*current = tasks[0]
	return current, nil
}

// Create allocates the key, inherits the parent's project, and stores the task.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Task, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, httpx.BadRequest("Title is required")
	}

	if err := validateTimeBox(input.StartTime, input.EndTime); err != nil {
		return nil, err
	}

	syncToPepy := input.SyncToPepy != nil && *input.SyncToPepy
	if syncToPepy {
		if !s.syncAvailable(ctx, userID) {
			return nil, httpx.BadRequest("Pepy sync is not available")
		}
		if input.DueDate == nil || input.StartTime == nil || *input.StartTime == "" ||
			input.EndTime == nil || *input.EndTime == "" {
			return nil, httpx.BadRequest("Send to Pepy requires a due date, start time, and end time")
		}
	}

	var created Task

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		var parent *Task
		if input.ParentID != nil {
			found, err := s.repo.GetForUpdate(ctx, tx, userID, *input.ParentID)
			if err != nil {
				return err
			}
			if found == nil {
				return httpx.BadRequest("Parent task not found")
			}
			if found.ParentID != nil {
				return httpx.BadRequest("Subtasks cannot have subtasks")
			}
			parent = found
		}

		projectID := input.ProjectID
		if parent != nil {
			projectID = parent.ProjectID
		}

		if projectID != nil {
			if err := s.requireProject(ctx, tx, userID, *projectID); err != nil {
				return err
			}
		}
		if input.AreaID != nil {
			if err := s.requireArea(ctx, tx, userID, *input.AreaID); err != nil {
				return err
			}
		}

		var dueDate *wiretime.Date
		if input.DueDate != nil {
			parsed, err := wiretime.ParseDate(*input.DueDate)
			if err != nil {
				return httpx.BadRequest("Use a YYYY-MM-DD date")
			}
			dueDate = &parsed
		}

		prefix := "SB"
		if projectID != nil {
			key, err := s.projects.Key(ctx, tx, userID, *projectID)
			if err != nil {
				return err
			}
			if key == nil {
				return httpx.BadRequest("Project not found")
			}
			prefix = *key
		}

		bucket := "none"
		if projectID != nil {
			bucket = *projectID
		}

		key := ""
		for range keyAttempts {
			next, err := s.counters.Next(ctx, tx, userID, bucket)
			if err != nil {
				return err
			}

			candidate := fmt.Sprintf("%s-%d", prefix, next)
			taken, err := s.repo.KeyTaken(ctx, tx, userID, candidate)
			if err != nil {
				return err
			}
			if !taken {
				key = candidate
				break
			}
		}
		if key == "" {
			return httpx.Conflict("Could not allocate a task key")
		}

		now := wiretime.Now()

		created = Task{
			ID:          ids.New("t"),
			Key:         key,
			Title:       title,
			Description: input.Description,
			Type:        "task",
			Status:      "todo",
			Priority:    "medium",
			DueDate:     dueDate,
			StartTime:   input.StartTime,
			EndTime:     input.EndTime,
			ProjectID:   projectID,
			AreaID:      input.AreaID,
			Labels:      CleanLabels(input.Labels),
			SyncToPepy:  syncToPepy,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if input.Type != nil {
			created.Type = *input.Type
		}
		if input.Status != nil {
			created.Status = *input.Status
		}
		if input.Priority != nil {
			created.Priority = *input.Priority
		}
		if parent != nil {
			created.ParentID = &parent.ID
		}

		row := Row{UserID: userID}
		row.Load(created)

		return s.repo.Insert(ctx, tx, row)
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	if syncToPepy {
		if s.taskSync == nil {
			return nil, httpx.BadRequest("Pepy sync is not available")
		}
		if err := s.taskSync.SetEnabled(ctx, userID, created.ID, true); err != nil {
			s.log.Warn("task sync opt-in failed", "user_id", userID, "task_id", created.ID, "error", err)
		} else {
			s.push(ctx, userID, created)
		}
	}

	return &created, nil
}

// Update applies the PATCH body in a fixed order: the parent patch wins over
// the project patch and adopts the parent's project.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Task, error) {
	var updated Task
	wasEnabled := s.syncEnabled(ctx, userID, id)

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Task not found")
		}

		if patch.Title != nil {
			if strings.TrimSpace(*patch.Title) == "" {
				return httpx.BadRequest("Title is required")
			}
			row.Title = *patch.Title
		}
		if patch.Description != nil {
			row.Description = patch.Description
		}
		if patch.Type != nil {
			row.Type = *patch.Type
		}
		if patch.Status != nil {
			row.Status = *patch.Status
		}
		if patch.Priority != nil {
			row.Priority = *patch.Priority
		}
		if patch.Labels != nil {
			row.Labels = CleanLabels(patch.Labels)
		}

		if patch.DueDate.Set {
			if patch.DueDate.Null {
				row.DueDate = nil
			} else {
				parsed, err := wiretime.ParseDate(patch.DueDate.Value)
				if err != nil {
					return httpx.BadRequest("Use a YYYY-MM-DD date")
				}
				row.DueDate = &parsed
			}
		}

		if patch.StartTime.Set {
			row.StartTime = nil
			if !patch.StartTime.Null {
				value := patch.StartTime.Value
				row.StartTime = &value
			}
		}

		if patch.EndTime.Set {
			row.EndTime = nil
			if !patch.EndTime.Null {
				value := patch.EndTime.Value
				row.EndTime = &value
			}
		}

		if err := validateTimeBox(row.StartTime, row.EndTime); err != nil {
			return err
		}

		if patch.ProjectID.Set {
			row.ProjectID = nil
			if !patch.ProjectID.Null {
				value := patch.ProjectID.Value
				if err := s.requireProject(ctx, tx, userID, value); err != nil {
					return err
				}
				row.ProjectID = &value
			}
		}

		if patch.AreaID.Set {
			row.AreaID = nil
			if !patch.AreaID.Null {
				value := patch.AreaID.Value
				if err := s.requireArea(ctx, tx, userID, value); err != nil {
					return err
				}
				row.AreaID = &value
			}
		}

		if patch.ParentID.Set {
			if patch.ParentID.Null {
				row.ParentID = nil
			} else {
				parentID := patch.ParentID.Value
				if parentID == id {
					return httpx.BadRequest("A task cannot be its own parent")
				}

				hasChildren, err := s.repo.HasChildren(ctx, tx, id)
				if err != nil {
					return err
				}
				if hasChildren {
					return httpx.BadRequest("A task with subtasks cannot become a subtask")
				}

				parent, err := s.repo.GetForUpdate(ctx, tx, userID, parentID)
				if err != nil {
					return err
				}
				if parent == nil {
					return httpx.BadRequest("Parent task not found")
				}
				if parent.ParentID != nil {
					return httpx.BadRequest("Subtasks cannot have subtasks")
				}

				row.ParentID = &parent.ID
				row.ProjectID = parent.ProjectID
			}
		}

		willEnable := wasEnabled
		if patch.SyncToPepy != nil {
			willEnable = *patch.SyncToPepy
		}
		if willEnable {
			if !s.syncAvailable(ctx, userID) {
				return httpx.BadRequest("Pepy sync is not available")
			}
			if row.DueDate == nil || row.StartTime == nil || *row.StartTime == "" ||
				row.EndTime == nil || *row.EndTime == "" {
				return httpx.BadRequest("Send to Pepy requires a due date, start time, and end time")
			}
		}

		row.UpdatedAt = wiretime.Now()

		saved := Row{UserID: userID}
		saved.Load(*row)

		if err := s.repo.Save(ctx, tx, saved); err != nil {
			return err
		}

		updated = *row
		updated.SyncToPepy = willEnable
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	if patch.SyncToPepy != nil && s.taskSync != nil {
		if err := s.taskSync.SetEnabled(ctx, userID, updated.ID, *patch.SyncToPepy); err != nil {
			s.log.Warn("task sync toggle failed", "user_id", userID, "task_id", updated.ID, "error", err)
		}
	}
	if updated.SyncToPepy {
		s.push(ctx, userID, updated)
	}

	return &updated, nil
}

// Remove deletes the task.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	if s.taskSync != nil {
		if err := s.taskSync.Delete(ctx, userID, id); err != nil {
			s.log.Warn("task sync delete failed", "user_id", userID, "task_id", id, "error", err)
		}
	}

	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return httpx.Map(err, s.log)
	}
	if s.attachments != nil {
		s.attachments.DeleteForEntity(ctx, userID, "task", id)
	}
	return nil
}

func (s *Service) syncAvailable(ctx context.Context, userID string) bool {
	return s.taskSync != nil && s.taskSync.Available(ctx, userID)
}

func (s *Service) syncEnabled(ctx context.Context, userID, taskID string) bool {
	if s.taskSync == nil {
		return false
	}
	ids, err := s.taskSync.EnabledTaskIDs(ctx, userID)
	if err != nil {
		s.log.Warn("task sync state read failed", "user_id", userID, "error", err)
		return false
	}
	return ids[taskID]
}

func (s *Service) attachSync(ctx context.Context, userID string, tasks []Task) {
	if s.taskSync == nil || len(tasks) == 0 {
		return
	}
	ids, err := s.taskSync.EnabledTaskIDs(ctx, userID)
	if err != nil {
		s.log.Warn("task sync state read failed", "user_id", userID, "error", err)
		return
	}
	for index := range tasks {
		tasks[index].SyncToPepy = ids[tasks[index].ID]
	}
}

func (s *Service) push(ctx context.Context, userID string, current Task) {
	if s.taskSync == nil || !current.SyncToPepy {
		return
	}
	if err := s.taskSync.Push(ctx, userID, current); err != nil {
		s.log.Warn("task sync push failed", "user_id", userID, "task_id", current.ID, "error", err)
	}
}

func (s *Service) requireProject(ctx context.Context, tx *gorm.DB, userID, id string) error {
	exists, err := s.projects.Exists(ctx, tx, userID, id)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.BadRequest("Project not found")
	}

	return nil
}

func (s *Service) requireArea(ctx context.Context, tx *gorm.DB, userID, id string) error {
	exists, err := s.areas.Exists(ctx, tx, userID, id)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.BadRequest("Area not found")
	}

	return nil
}
