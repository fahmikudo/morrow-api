// Package search powers the global command palette across every entity kind.
package search

import (
	"context"
	"log/slog"
	"strings"

	"morrow-api/internal/module/area"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/htmltext"
	"morrow-api/internal/platform/httpx"
)

// PerKindLimit caps how many rows of each kind the palette shows.
const PerKindLimit = 5

// SubtitleLength caps the description fragment shown under a result.
const SubtitleLength = 60

// Result is one palette row.
type Result struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Href     string `json:"href"`
}

// Listers keep the service independent of repositories it never writes to.
type TaskLister interface {
	List(ctx context.Context, userID string) ([]task.Task, error)
}

// NoteLister lists notes.
type NoteLister interface {
	List(ctx context.Context, userID string) ([]note.Note, error)
}

// ProjectLister lists projects.
type ProjectLister interface {
	List(ctx context.Context, userID string) ([]project.Project, error)
}

// InboxLister lists the active inbox items.
type InboxLister interface {
	ListActive(ctx context.Context, userID string) ([]inbox.Item, error)
}

// AreaLister lists areas.
type AreaLister interface {
	List(ctx context.Context, userID string) ([]area.Area, error)
}

// HabitLister lists the active habits.
type HabitLister interface {
	ListActive(ctx context.Context, userID string) ([]habit.Habit, error)
}

// Service matches the needle against every entity kind.
type Service struct {
	log      *slog.Logger
	tasks    TaskLister
	notes    NoteLister
	projects ProjectLister
	inbox    InboxLister
	areas    AreaLister
	habits   HabitLister
}

// NewService wires the search service.
func NewService(
	log *slog.Logger,
	tasks TaskLister,
	notes NoteLister,
	projects ProjectLister,
	inbox InboxLister,
	areas AreaLister,
	habits HabitLister,
) *Service {
	return &Service{
		log:      log,
		tasks:    tasks,
		notes:    notes,
		projects: projects,
		inbox:    inbox,
		areas:    areas,
		habits:   habits,
	}
}

// Search returns the matches in contract order: task, note, project, inbox,
// habit, area.
func (s *Service) Search(ctx context.Context, userID, query string) ([]Result, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return []Result{}, nil
	}

	taskRows, err := s.tasks.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	noteRows, err := s.notes.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	projectRows, err := s.projects.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	inboxRows, err := s.inbox.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	areaRows, err := s.areas.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	habitRows, err := s.habits.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	projectNames := make(map[string]string, len(projectRows))
	for _, current := range projectRows {
		projectNames[current.ID] = current.Name
	}

	results := make([]Result, 0, PerKindLimit*6)

	matchedTasks := 0
	for _, current := range taskRows {
		if matchedTasks == PerKindLimit || !matchesTask(current, needle) {
			continue
		}

		var detail *string
		if current.ProjectID != nil {
			if name, ok := projectNames[*current.ProjectID]; ok {
				detail = &name
			}
		} else {
			description := htmltext.StripHTML(deref(current.Description))
			detail = &description
		}

		results = append(results, Result{
			Kind:     "task",
			ID:       current.ID,
			Title:    current.Title,
			Subtitle: current.Key + " · " + subtitleFrom(detail, "Task"),
			Href:     "/tasks",
		})
		matchedTasks++
	}

	matchedNotes := 0
	for _, current := range noteRows {
		if matchedNotes == PerKindLimit {
			break
		}
		if !strings.Contains(strings.ToLower(current.Title), needle) &&
			!strings.Contains(strings.ToLower(htmltext.StripHTML(current.Content)), needle) &&
			!anyContains(current.Tags, needle) {
			continue
		}

		subtitle := htmltext.Excerpt(current.Content, SubtitleLength)
		title := strings.TrimSpace(current.Title)
		if title == "" {
			title = "Untitled"
		}
		results = append(results, Result{
			Kind:     "note",
			ID:       current.ID,
			Title:    title,
			Subtitle: subtitleFrom(&subtitle, "Note"),
			Href:     "/knowledge/" + current.ID,
		})
		matchedNotes++
	}

	matchedProjects := 0
	for _, current := range projectRows {
		if matchedProjects == PerKindLimit {
			break
		}
		description := htmltext.StripHTML(deref(current.Description))
		if !strings.Contains(strings.ToLower(current.Name), needle) &&
			!strings.Contains(strings.ToLower(description), needle) {
			continue
		}

		results = append(results, Result{
			Kind:     "project",
			ID:       current.ID,
			Title:    current.Name,
			Subtitle: current.Key + " · " + subtitleFrom(&description, "Project"),
			Href:     "/projects/" + current.ID,
		})
		matchedProjects++
	}

	matchedInbox := 0
	for _, current := range inboxRows {
		if matchedInbox == PerKindLimit {
			break
		}
		if !strings.Contains(strings.ToLower(current.Title), needle) &&
			!strings.Contains(strings.ToLower(deref(current.Content)), needle) {
			continue
		}

		results = append(results, Result{
			Kind:     "inbox",
			ID:       current.ID,
			Title:    current.Title,
			Subtitle: subtitleFrom(current.Content, "Inbox"),
			Href:     "/inbox",
		})
		matchedInbox++
	}

	matchedHabits := 0
	for _, current := range habitRows {
		if matchedHabits == PerKindLimit {
			break
		}
		if !strings.Contains(strings.ToLower(current.Title), needle) &&
			!strings.Contains(strings.ToLower(deref(current.Description)), needle) {
			continue
		}

		subtitle := htmltext.HabitCadenceLabel(current.Cadence, current.Target)
		if current.ProjectID != nil {
			if name, ok := projectNames[*current.ProjectID]; ok {
				subtitle += " · " + name
			}
		}

		results = append(results, Result{
			Kind:     "habit",
			ID:       current.ID,
			Title:    current.Title,
			Subtitle: subtitle,
			Href:     "/habits",
		})
		matchedHabits++
	}

	matchedAreas := 0
	for _, current := range areaRows {
		if matchedAreas == PerKindLimit {
			break
		}
		if !strings.Contains(strings.ToLower(current.Name), needle) &&
			!strings.Contains(strings.ToLower(deref(current.Description)), needle) {
			continue
		}

		results = append(results, Result{
			Kind:     "area",
			ID:       current.ID,
			Title:    current.Name,
			Subtitle: subtitleFrom(current.Description, "Area"),
			Href:     "/areas/" + current.ID,
		})
		matchedAreas++
	}

	return results, nil
}

func matchesTask(current task.Task, needle string) bool {
	return strings.Contains(strings.ToLower(current.Title), needle) ||
		strings.Contains(strings.ToLower(current.Key), needle) ||
		anyContains(current.Labels, needle) ||
		strings.Contains(strings.ToLower(htmltext.StripHTML(deref(current.Description))), needle)
}

func subtitleFrom(value *string, fallback string) string {
	if value == nil {
		return fallback
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}

	return htmltext.Truncate(trimmed, SubtitleLength)
}

func anyContains(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}

	return false
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
