package habit

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/project"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the habit rules inside one transaction per write.
type Service struct {
	db       *gorm.DB
	log      *slog.Logger
	repo     *Repository
	projects *project.Repository
	areas    *area.Repository
}

// NewService wires the habit service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	projects *project.Repository,
	areas *area.Repository,
) *Service {
	return &Service{db: db, log: log, repo: repo, projects: projects, areas: areas}
}

// List returns the active habits.
func (s *Service) List(ctx context.Context, userID string) ([]Habit, error) {
	habits, err := s.repo.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return habits, nil
}

// ListArchived returns the archived habits.
func (s *Service) ListArchived(ctx context.Context, userID string) ([]Habit, error) {
	habits, err := s.repo.ListArchived(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return habits, nil
}

// Get returns one habit or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Habit, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Habit not found")
	}

	return current, nil
}

// Create validates the schedule and stores the trimmed title.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Habit, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, httpx.BadRequest("Title is required")
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
	if input.ReminderLeadMinutes != nil {
		if err := validateReminderLead(*input.ReminderLeadMinutes); err != nil {
			return nil, err
		}
	}

	startDate := DateOf(time.Now().UTC())
	if input.StartDate != nil {
		parsed, err := wiretime.ParseDate(*input.StartDate)
		if err != nil {
			return nil, httpx.BadRequest("Use a YYYY-MM-DD date")
		}
		startDate = parsed
	}

	var endDate *wiretime.Date
	if input.EndDate != nil {
		parsed, err := wiretime.ParseDate(*input.EndDate)
		if err != nil {
			return nil, httpx.BadRequest("Use a YYYY-MM-DD date")
		}
		if parsed.String() < startDate.String() {
			return nil, httpx.BadRequest("End date must be on or after the start date")
		}
		endDate = &parsed
	}

	schedule := Schedule{Kind: "flexible"}
	if input.Schedule != nil {
		schedule = *input.Schedule
	}
	if err := validateSchedule(schedule); err != nil {
		return nil, err
	}

	var created Habit

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if input.AreaID != nil {
			if err := s.requireArea(ctx, tx, userID, *input.AreaID); err != nil {
				return err
			}
		}
		if input.ProjectID != nil {
			if err := s.requireProject(ctx, tx, userID, *input.ProjectID); err != nil {
				return err
			}
		}

		now := wiretime.Now()

		created = Habit{
			ID:                  ids.New("h"),
			Title:               strings.TrimSpace(input.Title),
			Description:         input.Description,
			AreaID:              input.AreaID,
			ProjectID:           input.ProjectID,
			Cadence:             "weekly",
			Target:              1,
			Schedule:            schedule,
			StartTime:           input.StartTime,
			EndTime:             input.EndTime,
			ReminderEnabled:     input.ReminderEnabled,
			ReminderLeadMinutes: input.ReminderLeadMinutes,
			StartDate:           startDate,
			EndDate:             endDate,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if input.Cadence != nil {
			created.Cadence = *input.Cadence
		}
		if input.Target != nil {
			created.Target = *input.Target
		}
		if created.Target < 1 {
			created.Target = 1
		}

		row := Row{UserID: userID}
		row.Load(created)

		return s.repo.Insert(ctx, tx, row)
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &created, nil
}

// Update applies the PATCH body in a fixed order and validates the final
// schedule last, so a bad area still wins over a bad schedule.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Habit, error) {
	var updated Habit

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Habit not found")
		}

		habit := row.ToWire()

		if patch.Title != nil {
			if strings.TrimSpace(*patch.Title) == "" {
				return httpx.BadRequest("Title is required")
			}
			habit.Title = *patch.Title
		}
		if patch.Description != nil {
			habit.Description = patch.Description
		}
		if patch.Cadence != nil {
			habit.Cadence = *patch.Cadence
		}
		if patch.Target != nil {
			habit.Target = roundTarget(*patch.Target)
		}
		if patch.Schedule != nil {
			habit.Schedule = *patch.Schedule
		}
		if patch.StartTime.Set {
			habit.StartTime = nil
			if !patch.StartTime.Null {
				value := patch.StartTime.Value
				if err := wiretime.ValidateTimeOfDay(value); err != nil {
					return httpx.BadRequest("Use an HH:mm time")
				}
				habit.StartTime = &value
			}
		}
		if patch.EndTime.Set {
			habit.EndTime = nil
			if !patch.EndTime.Null {
				value := patch.EndTime.Value
				if err := wiretime.ValidateTimeOfDay(value); err != nil {
					return httpx.BadRequest("Use an HH:mm time")
				}
				habit.EndTime = &value
			}
		}
		if patch.ReminderEnabled.Set {
			habit.ReminderEnabled = nil
			if !patch.ReminderEnabled.Null {
				value := patch.ReminderEnabled.Value
				habit.ReminderEnabled = &value
			}
		}
		if patch.ReminderLeadMinutes.Set {
			habit.ReminderLeadMinutes = nil
			if !patch.ReminderLeadMinutes.Null {
				value := patch.ReminderLeadMinutes.Value
				if err := validateReminderLead(value); err != nil {
					return err
				}
				habit.ReminderLeadMinutes = &value
			}
		}
		if patch.StartDate != nil {
			parsed, err := wiretime.ParseDate(*patch.StartDate)
			if err != nil {
				return httpx.BadRequest("Use a YYYY-MM-DD date")
			}
			habit.StartDate = parsed
		}
		if patch.EndDate.Set {
			habit.EndDate = nil
			if !patch.EndDate.Null {
				parsed, err := wiretime.ParseDate(patch.EndDate.Value)
				if err != nil {
					return httpx.BadRequest("Use a YYYY-MM-DD date")
				}
				habit.EndDate = &parsed
			}
		}
		if habit.EndDate != nil && habit.EndDate.String() < habit.StartDate.String() {
			return httpx.BadRequest("End date must be on or after the start date")
		}
		if patch.AreaID.Set {
			habit.AreaID = nil
			if !patch.AreaID.Null {
				value := patch.AreaID.Value
				if err := s.requireArea(ctx, tx, userID, value); err != nil {
					return err
				}
				habit.AreaID = &value
			}
		}
		if patch.ProjectID.Set {
			habit.ProjectID = nil
			if !patch.ProjectID.Null {
				value := patch.ProjectID.Value
				if err := s.requireProject(ctx, tx, userID, value); err != nil {
					return err
				}
				habit.ProjectID = &value
			}
		}
		if patch.ArchivedAt.Set {
			habit.ArchivedAt = nil
			if !patch.ArchivedAt.Null {
				parsed, err := wiretime.ParseTimestamp(patch.ArchivedAt.Value)
				if err != nil {
					return httpx.BadRequest("Use an RFC 3339 timestamp")
				}
				habit.ArchivedAt = &parsed
			}
		}

		if err := validateSchedule(habit.Schedule); err != nil {
			return err
		}

		habit.UpdatedAt = wiretime.Now()

		saved := Row{UserID: userID}
		saved.Load(habit)

		if err := s.repo.Save(ctx, tx, saved); err != nil {
			return err
		}

		updated = habit
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

// Archive stamps `archived_at` once and keeps the first instant on repeats.
func (s *Service) Archive(ctx context.Context, userID, id string) (*Habit, error) {
	return s.flipArchive(ctx, userID, id, true)
}

// Restore clears `archived_at`.
func (s *Service) Restore(ctx context.Context, userID, id string) (*Habit, error) {
	return s.flipArchive(ctx, userID, id, false)
}

func (s *Service) flipArchive(ctx context.Context, userID, id string, archived bool) (*Habit, error) {
	var updated Habit

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Habit not found")
		}

		habit := row.ToWire()
		if archived {
			if habit.ArchivedAt == nil {
				now := wiretime.Now()
				habit.ArchivedAt = &now
			}
		} else {
			habit.ArchivedAt = nil
		}
		habit.UpdatedAt = wiretime.Now()

		saved := Row{UserID: userID}
		saved.Load(habit)

		if err := s.repo.Save(ctx, tx, saved); err != nil {
			return err
		}

		updated = habit
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

// Remove deletes the habit; its check-ins cascade.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	return httpx.Map(s.repo.Delete(ctx, userID, id), s.log)
}

// ListCheckIns returns every check-in.
func (s *Service) ListCheckIns(ctx context.Context, userID string) ([]CheckIn, error) {
	checkIns, err := s.repo.ListCheckIns(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return checkIns, nil
}

// CheckIn records one habit for one day; repeating the call returns the
// original row.
func (s *Service) CheckIn(ctx context.Context, userID, habitID, rawDate string) (*CheckIn, error) {
	date, err := wiretime.ParseDate(rawDate)
	if err != nil {
		return nil, httpx.BadRequest("Use a YYYY-MM-DD date for check-ins")
	}

	var stored *CheckIn

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.requireHabit(ctx, tx, userID, habitID); err != nil {
			return err
		}

		if err := s.repo.DeleteSkip(ctx, tx, userID, habitID, date.Time()); err != nil {
			return err
		}

		checkIn, err := s.repo.UpsertCheckIn(ctx, tx, userID, habitID, date.Time(), ids.New("hc"), wiretime.Now().Time())
		if err != nil {
			return err
		}

		stored = checkIn
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return stored, nil
}

// RemoveCheckIn deletes one day's check-in; a missing row still returns 204.
func (s *Service) RemoveCheckIn(ctx context.Context, userID, habitID, rawDate string) error {
	date, err := wiretime.ParseDate(rawDate)
	if err != nil {
		return httpx.BadRequest("Use a YYYY-MM-DD date for check-ins")
	}

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.requireHabit(ctx, tx, userID, habitID); err != nil {
			return err
		}

		return s.repo.DeleteCheckIn(ctx, tx, userID, habitID, date.Time())
	})

	return httpx.Map(err, s.log)
}

// ListSkips returns every explicit "not today" row.
func (s *Service) ListSkips(ctx context.Context, userID string) ([]Skip, error) {
	skips, err := s.repo.ListSkips(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return skips, nil
}

// Skip records one habit as skipped for one day; repeating the call returns
// the original row and clears any check-in for the same day.
func (s *Service) Skip(ctx context.Context, userID, habitID, rawDate string) (*Skip, error) {
	date, err := wiretime.ParseDate(rawDate)
	if err != nil {
		return nil, httpx.BadRequest("Use a YYYY-MM-DD date for skips")
	}

	var stored *Skip

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.requireHabit(ctx, tx, userID, habitID); err != nil {
			return err
		}

		if err := s.repo.DeleteCheckIn(ctx, tx, userID, habitID, date.Time()); err != nil {
			return err
		}

		skip, err := s.repo.UpsertSkip(ctx, tx, userID, habitID, date.Time(), ids.New("hs"), wiretime.Now().Time())
		if err != nil {
			return err
		}

		stored = skip
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return stored, nil
}

// RemoveSkip deletes one day's skip; a missing row still returns 204.
func (s *Service) RemoveSkip(ctx context.Context, userID, habitID, rawDate string) error {
	date, err := wiretime.ParseDate(rawDate)
	if err != nil {
		return httpx.BadRequest("Use a YYYY-MM-DD date for skips")
	}

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.requireHabit(ctx, tx, userID, habitID); err != nil {
			return err
		}

		return s.repo.DeleteSkip(ctx, tx, userID, habitID, date.Time())
	})

	return httpx.Map(err, s.log)
}

// DateOf keeps only the UTC calendar date of an instant.
func DateOf(value time.Time) wiretime.Date {
	utc := value.UTC()
	return wiretime.Date(time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC))
}

// validateReminderLead keeps the lead inside the same day-scale bound as the
// global event lead.
func validateReminderLead(minutes int32) error {
	if minutes < 0 || minutes > 1440 {
		return httpx.BadRequest("Reminder lead must be between 0 and 1440 minutes")
	}

	return nil
}

func validateSchedule(schedule Schedule) error {
	if schedule.Kind != "fixed" {
		return nil
	}
	if len(schedule.Weekdays) == 0 {
		return httpx.BadRequest("Pick at least one weekday")
	}
	for _, weekday := range schedule.Weekdays {
		if weekday < 0 || weekday > 6 {
			return httpx.BadRequest("Weekdays must be between 0 and 6")
		}
	}

	return nil
}

// roundTarget mirrors Rust's `(target.round() as i32).max(1)` including the
// saturating cast.
func roundTarget(value float64) int32 {
	rounded := math.Round(value)
	if rounded < 1 {
		return 1
	}
	if rounded > math.MaxInt32 {
		return math.MaxInt32
	}

	return int32(rounded)
}

func (s *Service) requireHabit(ctx context.Context, tx *gorm.DB, userID, id string) error {
	exists, err := s.repo.Exists(ctx, tx, userID, id)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.NotFound("Habit not found")
	}

	return nil
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
