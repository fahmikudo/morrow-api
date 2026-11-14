package snapshot

import (
	"context"
	"log/slog"

	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Listers are the read-only slices of each repository the snapshot needs.
type (
	TaskLister interface {
		List(ctx context.Context, userID string) ([]task.Task, error)
	}
	NoteLister interface {
		List(ctx context.Context, userID string) ([]note.Note, error)
	}
	ProjectLister interface {
		List(ctx context.Context, userID string) ([]project.Project, error)
	}
	EventLister interface {
		List(ctx context.Context, userID string) ([]event.Event, error)
	}
	AreaLister interface {
		List(ctx context.Context, userID string) ([]area.Area, error)
	}
	InboxLister interface {
		ListAll(ctx context.Context, userID string) ([]inbox.Item, error)
	}
	HabitLister interface {
		ListAll(ctx context.Context, userID string) ([]habit.Habit, error)
	}
	CheckInLister interface {
		ListCheckIns(ctx context.Context, userID string) ([]habit.CheckIn, error)
	}
	SkipLister interface {
		ListSkips(ctx context.Context, userID string) ([]habit.Skip, error)
	}
	ProfileReader interface {
		Get(ctx context.Context, userID string) (*profile.Profile, error)
	}
	CounterLister interface {
		List(ctx context.Context, userID string) (map[string]int32, error)
	}
	SettingsProvider interface {
		Integrations(ctx context.Context, userID string) (map[string]settings.IntegrationConnection, error)
		AI(ctx context.Context, userID string) (settings.AISettings, *settings.AIRow, error)
	}
)

// Deps is everything the snapshot reads.
type Deps struct {
	Tasks    TaskLister
	Notes    NoteLister
	Projects ProjectLister
	Events   EventLister
	Areas    AreaLister
	Inbox    InboxLister
	Habits   HabitLister
	CheckIns CheckInLister
	Skips    SkipLister
	Profiles ProfileReader
	Counters CounterLister
	Settings SettingsProvider
}

// Service assembles the snapshot from the module readers.
type Service struct {
	log  *slog.Logger
	deps Deps
}

// NewService wires the snapshot service.
func NewService(log *slog.Logger, deps Deps) *Service {
	return &Service{log: log, deps: deps}
}

// Snapshot reads every list in one pass; a missing profile is a 404.
func (s *Service) Snapshot(ctx context.Context, userID string) (*Response, error) {
	currentProfile, err := s.deps.Profiles.Get(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if currentProfile == nil {
		return nil, httpx.NotFound("Profile not found")
	}

	inboxItems, err := s.deps.Inbox.ListAll(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	tasks, err := s.deps.Tasks.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	projects, err := s.deps.Projects.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	notes, err := s.deps.Notes.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	events, err := s.deps.Events.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	areas, err := s.deps.Areas.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	habits, err := s.deps.Habits.ListAll(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	checkIns, err := s.deps.CheckIns.ListCheckIns(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	skips, err := s.deps.Skips.ListSkips(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	counters, err := s.deps.Counters.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	integrations, err := s.deps.Settings.Integrations(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	ai, _, err := s.deps.Settings.AI(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &Response{
		Version:       Version,
		Inbox:         inboxItems,
		Tasks:         tasks,
		Projects:      projects,
		Notes:         notes,
		Events:        events,
		Areas:         areas,
		Habits:        habits,
		HabitCheckIns: checkIns,
		HabitSkips:    skips,
		Profile:       *currentProfile,
		Counters:      counters,
		Integrations:  integrations,
		AI:            ai,
		ServerTime:    wiretime.Now(),
	}, nil
}
