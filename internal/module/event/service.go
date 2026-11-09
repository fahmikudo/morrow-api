package event

import (
	"context"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/project"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// CalendarSync mirrors event writes to a connected calendar provider. It is
// optional so tests and unconfigured deployments stay local.
type CalendarSync interface {
	Push(ctx context.Context, userID string, current Event) error
	Delete(ctx context.Context, userID, eventID string) error
}

// Service applies the event rules inside one transaction per write.
type Service struct {
	db       *gorm.DB
	log      *slog.Logger
	repo     *Repository
	projects *project.Repository
	calendar CalendarSync
}

// NewService wires the event service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	projects *project.Repository,
	calendar CalendarSync,
) *Service {
	return &Service{db: db, log: log, repo: repo, projects: projects, calendar: calendar}
}

// List returns every event in calendar order.
func (s *Service) List(ctx context.Context, userID string) ([]Event, error) {
	events, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return events, nil
}

// Get returns one event or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Event, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Event not found")
	}

	return current, nil
}

// Create validates the title, date, and time-of-day fields, then stores the
// event with the title exactly as provided.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Event, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, httpx.BadRequest("Title is required")
	}

	date, err := wiretime.ParseDate(input.Date)
	if err != nil {
		return nil, httpx.BadRequest("Use a YYYY-MM-DD date")
	}

	if input.StartTime != nil {
		if err := wiretime.ValidateTimeOfDay(*input.StartTime); err != nil {
			return nil, httpx.BadRequest("Use an HH:mm time")
		}
	}
	if input.EndTime != nil {
		if err := wiretime.ValidateTimeOfDay(*input.EndTime); err != nil {
			return nil, httpx.BadRequest("Use an HH:mm time")
		}
	}

	var created Event

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if input.ProjectID != nil {
			if err := s.requireProject(ctx, tx, userID, *input.ProjectID); err != nil {
				return err
			}
		}

		now := wiretime.Now()

		created = Event{
			ID:        ids.New("e"),
			Title:     input.Title,
			Date:      date,
			StartTime: input.StartTime,
			EndTime:   input.EndTime,
			Notes:     input.Notes,
			ProjectID: input.ProjectID,
			Source:    "local",
			CreatedAt: now,
			UpdatedAt: now,
		}

		row := Row{UserID: userID}
		row.Load(created)

		return s.repo.Insert(ctx, tx, row)
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	s.push(ctx, userID, created)
	return &created, nil
}

// Update applies a PATCH body; the title is accepted unvalidated.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Event, error) {
	var updated Event

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Event not found")
		}

		if patch.Title != nil {
			row.Title = *patch.Title
		}
		if patch.Date != nil {
			date, err := wiretime.ParseDate(*patch.Date)
			if err != nil {
				return httpx.BadRequest("Use a YYYY-MM-DD date")
			}
			row.Date = date.Time()
		}
		if patch.Notes != nil {
			row.Notes = patch.Notes
		}
		if patch.StartTime.Set {
			row.StartTime = nil
			if !patch.StartTime.Null {
				value := patch.StartTime.Value
				if err := wiretime.ValidateTimeOfDay(value); err != nil {
					return httpx.BadRequest("Use an HH:mm time")
				}
				clock := wiretime.Clock(value)
				row.StartTime = &clock
			}
		}
		if patch.EndTime.Set {
			row.EndTime = nil
			if !patch.EndTime.Null {
				value := patch.EndTime.Value
				if err := wiretime.ValidateTimeOfDay(value); err != nil {
					return httpx.BadRequest("Use an HH:mm time")
				}
				clock := wiretime.Clock(value)
				row.EndTime = &clock
			}
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

		row.UpdatedAt = wiretime.Now().Time()

		if err := s.repo.Save(ctx, tx, *row); err != nil {
			return err
		}

		updated = row.ToWire()
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	s.push(ctx, userID, updated)
	return &updated, nil
}

// Remove deletes the event. A Google delete is best-effort; a failure leaves a
// link tombstone that the next sync retries.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	if s.calendar != nil {
		if err := s.calendar.Delete(ctx, userID, id); err != nil {
			s.log.Warn("calendar delete failed", "user_id", userID, "event_id", id, "error", err)
		}
	}

	return httpx.Map(s.repo.Delete(ctx, userID, id), s.log)
}

func (s *Service) push(ctx context.Context, userID string, current Event) {
	if s.calendar == nil {
		return
	}
	if err := s.calendar.Push(ctx, userID, current); err != nil {
		s.log.Warn("calendar push failed", "user_id", userID, "event_id", current.ID, "error", err)
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
