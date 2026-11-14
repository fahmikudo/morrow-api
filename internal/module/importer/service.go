package importer

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Deps is every repository the import writes through.
type Deps struct {
	Areas    *area.Repository
	Projects *project.Repository
	Inbox    *inbox.Repository
	Tasks    *task.Repository
	Notes    *note.Repository
	Events   *event.Repository
	Habits   *habit.Repository
	Counters *task.Counters
	Profiles *profile.Repository
}

// Service validates the envelope, then replaces the dataset in one transaction.
type Service struct {
	db   *gorm.DB
	log  *slog.Logger
	repo *Repository
	deps Deps
}

// NewService wires the importer.
func NewService(db *gorm.DB, log *slog.Logger, repo *Repository, deps Deps) *Service {
	return &Service{db: db, log: log, repo: repo, deps: deps}
}

// Import validates and writes the payload.
func (s *Service) Import(ctx context.Context, userID string, payload Database) error {
	if err := Validate(payload); err != nil {
		return err
	}

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.repo.Clear(ctx, tx, userID); err != nil {
			return err
		}

		for _, current := range payload.Areas {
			row := area.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Areas.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		for _, current := range payload.Projects {
			row := project.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Projects.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		for _, current := range payload.Inbox {
			row := inbox.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Inbox.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		// Parent rows first: the self-referencing FK needs the parent to exist.
		for _, current := range payload.Tasks {
			parentless := current
			parentless.ParentID = nil

			row := task.Row{UserID: userID}
			row.Load(parentless)
			if err := s.deps.Tasks.Insert(ctx, tx, row); err != nil {
				return err
			}
		}
		for _, current := range payload.Tasks {
			if current.ParentID != nil {
				if err := s.deps.Tasks.SetParent(ctx, tx, userID, current.ID, current.ParentID); err != nil {
					return err
				}
			}
		}

		for _, current := range payload.Notes {
			row := note.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Notes.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		for _, current := range payload.Events {
			row := event.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Events.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		for _, current := range payload.Habits {
			row := habit.Row{UserID: userID}
			row.Load(current)
			if err := s.deps.Habits.Insert(ctx, tx, row); err != nil {
				return err
			}
		}

		for _, current := range payload.HabitCheckIns {
			_, err := s.deps.Habits.UpsertCheckIn(
				ctx,
				tx,
				userID,
				current.HabitID,
				current.Date.Time(),
				current.ID,
				current.CreatedAt.Time(),
			)
			if err != nil {
				return err
			}
		}

		for _, current := range payload.HabitSkips {
			_, err := s.deps.Habits.UpsertSkip(
				ctx,
				tx,
				userID,
				current.HabitID,
				current.Date.Time(),
				current.ID,
				current.CreatedAt.Time(),
			)
			if err != nil {
				return err
			}
		}

		for bucket, next := range counterBuckets(payload) {
			if err := s.deps.Counters.Set(ctx, tx, userID, bucket, next); err != nil {
				return err
			}
		}

		profileRow := profile.Row{UserID: userID}
		profileRow.Load(payload.Profile)
		if err := s.deps.Profiles.Save(ctx, tx, profileRow); err != nil {
			return err
		}

		return nil
	})

	return httpx.Map(err, s.log)
}

// counterBuckets keeps `max(provided, highest suffix observed on a task key)`.
func counterBuckets(payload Database) map[string]int32 {
	projectKeys := make(map[string]string, len(payload.Projects))
	for _, current := range payload.Projects {
		projectKeys[current.ID] = current.Key
	}

	buckets := make(map[string]int32, len(payload.Counters)+2)
	for bucket, next := range payload.Counters {
		buckets[bucket] = next
	}

	for _, current := range payload.Tasks {
		bucket := "none"
		prefix := "SB"
		if current.ProjectID != nil {
			bucket = *current.ProjectID
			if key, ok := projectKeys[*current.ProjectID]; ok {
				prefix = key
			}
		}

		marker := prefix + "-"
		suffix, found := strings.CutPrefix(current.Key, marker)
		if !found {
			continue
		}

		value, err := strconv.ParseInt(suffix, 10, 32)
		if err != nil {
			continue
		}

		if int32(value) > buckets[bucket] {
			buckets[bucket] = int32(value)
		}
	}

	return buckets
}

// Validate rejects every envelope the importer cannot write, in the order the
// contract defines. All failures are 400 `Invalid import: …`.
func Validate(payload Database) error {
	if payload.Version != 6 {
		return httpx.BadRequest("Invalid import: unsupported version")
	}

	seen := make(map[string]struct{})
	allIDs := make([]string, 0)
	for _, item := range payload.Inbox {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Tasks {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Projects {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Notes {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Events {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Areas {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.Habits {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.HabitCheckIns {
		allIDs = append(allIDs, item.ID)
	}
	for _, item := range payload.HabitSkips {
		allIDs = append(allIDs, item.ID)
	}

	for _, id := range allIDs {
		if _, exists := seen[id]; exists {
			return httpx.BadRequest("Invalid import: duplicate id " + id)
		}
		seen[id] = struct{}{}
	}

	projectIDs := make(map[string]struct{}, len(payload.Projects))
	for _, current := range payload.Projects {
		projectIDs[current.ID] = struct{}{}
	}
	areaIDs := make(map[string]struct{}, len(payload.Areas))
	for _, current := range payload.Areas {
		areaIDs[current.ID] = struct{}{}
	}
	habitIDs := make(map[string]struct{}, len(payload.Habits))
	for _, current := range payload.Habits {
		habitIDs[current.ID] = struct{}{}
	}
	taskParents := make(map[string]*string, len(payload.Tasks))
	for _, current := range payload.Tasks {
		taskParents[current.ID] = current.ParentID
	}

	missing := func(field, id string) error {
		return httpx.BadRequest("Invalid import: missing reference " + field + " for " + id)
	}

	for _, current := range payload.Projects {
		if current.AreaID != nil {
			if _, ok := areaIDs[*current.AreaID]; !ok {
				return missing("areaId", current.ID)
			}
		}
	}

	for _, current := range payload.Tasks {
		if current.ProjectID != nil {
			if _, ok := projectIDs[*current.ProjectID]; !ok {
				return missing("projectId", current.ID)
			}
		}
		if current.AreaID != nil {
			if _, ok := areaIDs[*current.AreaID]; !ok {
				return missing("areaId", current.ID)
			}
		}
		if current.ParentID != nil {
			parent, ok := taskParents[*current.ParentID]
			if !ok {
				return missing("parentId", current.ID)
			}
			if parent != nil {
				return httpx.BadRequest("Invalid import: subtask tree too deep")
			}
		}
	}

	for _, current := range payload.Notes {
		if current.ProjectID != nil {
			if _, ok := projectIDs[*current.ProjectID]; !ok {
				return missing("projectId", current.ID)
			}
		}
		if current.AreaID != nil {
			if _, ok := areaIDs[*current.AreaID]; !ok {
				return missing("areaId", current.ID)
			}
		}
	}

	for _, current := range payload.Events {
		if current.ProjectID != nil {
			if _, ok := projectIDs[*current.ProjectID]; !ok {
				return missing("projectId", current.ID)
			}
		}
	}

	for _, current := range payload.Habits {
		if current.AreaID != nil {
			if _, ok := areaIDs[*current.AreaID]; !ok {
				return missing("areaId", current.ID)
			}
		}
		if current.ProjectID != nil {
			if _, ok := projectIDs[*current.ProjectID]; !ok {
				return missing("projectId", current.ID)
			}
		}
		if current.EndDate != nil && current.EndDate.String() < current.StartDate.String() {
			return httpx.BadRequest("Invalid import: end date before start date for " + current.ID)
		}
	}

	for _, current := range payload.HabitCheckIns {
		if _, ok := habitIDs[current.HabitID]; !ok {
			return missing("habitId", current.ID)
		}
	}

	for _, current := range payload.HabitSkips {
		if _, ok := habitIDs[current.HabitID]; !ok {
			return missing("habitId", current.ID)
		}
	}

	type checkInKey struct {
		habitID string
		date    string
	}
	checkInKeys := make(map[checkInKey]struct{}, len(payload.HabitCheckIns))
	for _, current := range payload.HabitCheckIns {
		key := checkInKey{habitID: current.HabitID, date: current.Date.String()}
		if _, exists := checkInKeys[key]; exists {
			return httpx.BadRequest(
				"Invalid import: duplicate check-in for " + current.HabitID + " on " + current.Date.String(),
			)
		}
		checkInKeys[key] = struct{}{}
	}

	skipKeys := make(map[checkInKey]struct{}, len(payload.HabitSkips))
	for _, current := range payload.HabitSkips {
		key := checkInKey{habitID: current.HabitID, date: current.Date.String()}
		if _, exists := skipKeys[key]; exists {
			return httpx.BadRequest(
				"Invalid import: duplicate skip for " + current.HabitID + " on " + current.Date.String(),
			)
		}
		skipKeys[key] = struct{}{}
	}

	projectKeys := make(map[string]struct{}, len(payload.Projects))
	for _, current := range payload.Projects {
		if _, exists := projectKeys[current.Key]; exists {
			return httpx.BadRequest("Invalid import: duplicate project key " + current.Key)
		}
		projectKeys[current.Key] = struct{}{}
	}

	taskKeys := make(map[string]struct{}, len(payload.Tasks))
	for _, current := range payload.Tasks {
		if _, exists := taskKeys[current.Key]; exists {
			return httpx.BadRequest("Invalid import: duplicate task key " + current.Key)
		}
		taskKeys[current.Key] = struct{}{}
	}

	return nil
}

var _ = wiretime.Now
