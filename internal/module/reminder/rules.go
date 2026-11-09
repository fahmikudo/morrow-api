package reminder

import (
	"fmt"
	"time"

	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/task"
)

const dateLayout = "2006-01-02"

// candidate is one reminder the rules would deliver now.
type candidate struct {
	Kind  string
	RefID string
	Title string
	Body  string
	Href  string
	At    time.Time
}

// ruleData is every row the rules read for one user.
type ruleData struct {
	Tasks    []task.Task
	Events   []event.Event
	Habits   []habit.Habit
	CheckIns []habit.CheckIn
}

// computeDue returns the reminders scheduled at or before now in the user's
// local day: open tasks due today or earlier at the daily time, events at their
// start minus the lead, and habits at their start time on scheduled days whose
// period target is still unmet.
func computeDue(now time.Time, loc *time.Location, settings Settings, data ruleData) []candidate {
	if loc == nil {
		loc = time.UTC
	}

	today := localToday(now, loc)
	due := make([]candidate, 0)

	for _, item := range data.Tasks {
		if item.Status == "done" || item.DueDate == nil {
			continue
		}

		dueDate := item.DueDate.String()
		if dueDate > today {
			continue
		}

		at, ok := clockOn(today, settings.TaskTime, loc)
		if !ok || now.Before(at) {
			continue
		}

		title := "Task due today"
		if dueDate < today {
			title = "Overdue task"
		}

		due = append(due, candidate{
			Kind:  "task",
			RefID: item.ID,
			Title: title,
			Body:  item.Key + " · " + item.Title,
			Href:  "/tasks",
			At:    at,
		})
	}

	for _, item := range data.Events {
		if item.Date.String() != today || item.StartTime == nil || *item.StartTime == "" {
			continue
		}

		start, ok := clockOn(today, *item.StartTime, loc)
		if !ok {
			continue
		}

		at := start.Add(-time.Duration(settings.EventLeadMinutes) * time.Minute)
		if now.Before(at) || now.After(at.Add(15*time.Minute)) {
			continue
		}

		title := "Event starting now"
		if settings.EventLeadMinutes > 0 {
			title = fmt.Sprintf("Event starts in %d min", settings.EventLeadMinutes)
		}

		due = append(due, candidate{
			Kind:  "event",
			RefID: item.ID,
			Title: title,
			Body:  *item.StartTime + " · " + item.Title,
			Href:  "/calendar",
			At:    at,
		})
	}

	for _, item := range data.Habits {
		if item.ArchivedAt != nil || item.StartTime == nil || *item.StartTime == "" {
			continue
		}

		if item.StartDate.String() > today {
			continue
		}

		if !scheduledOn(item, today, loc) {
			continue
		}

		if checkedInToday(data.CheckIns, item.ID, today) {
			continue
		}

		period := periodCount(data.CheckIns, item.ID, item.Cadence, today, loc)
		if period >= item.Target {
			continue
		}

		at, ok := clockOn(today, *item.StartTime, loc)
		if !ok || now.Before(at) {
			continue
		}

		due = append(due, candidate{
			Kind:  "habit",
			RefID: item.ID,
			Title: "Habit reminder",
			Body:  *item.StartTime + " · " + item.Title,
			Href:  "/habits",
			At:    at,
		})
	}

	return due
}

// localToday renders the user's current calendar date.
func localToday(now time.Time, loc *time.Location) string {
	return now.In(loc).Format(dateLayout)
}

// clockOn resolves an `HH:mm` clock on a calendar date in the user's zone.
func clockOn(date, clock string, loc *time.Location) (time.Time, bool) {
	parsed, err := time.ParseInLocation(dateLayout+" 15:04", date+" "+clock, loc)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

// scheduledOn reports whether a habit's schedule includes the local date.
func scheduledOn(item habit.Habit, today string, loc *time.Location) bool {
	if item.Schedule.Kind != "fixed" {
		return true
	}

	date, err := time.ParseInLocation(dateLayout, today, loc)
	if err != nil {
		return false
	}

	weekday := int32(date.Weekday())
	for _, day := range item.Schedule.Weekdays {
		if day == weekday {
			return true
		}
	}

	return false
}

// checkedInToday reports whether the habit already has today's check-in.
func checkedInToday(checkIns []habit.CheckIn, habitID, today string) bool {
	for _, checkIn := range checkIns {
		if checkIn.HabitID == habitID && checkIn.Date.String() == today {
			return true
		}
	}

	return false
}

// periodCount counts the habit's check-ins in the cadence period holding today.
func periodCount(checkIns []habit.CheckIn, habitID, cadence, today string, loc *time.Location) int32 {
	start, end := periodBounds(cadence, today, loc)

	var count int32
	for _, checkIn := range checkIns {
		if checkIn.HabitID != habitID {
			continue
		}

		date := checkIn.Date.String()
		if date >= start && date <= end {
			count++
		}
	}

	return count
}

// periodBounds returns the inclusive date range of the cadence period.
func periodBounds(cadence, today string, loc *time.Location) (string, string) {
	date, err := time.ParseInLocation(dateLayout, today, loc)
	if err != nil {
		return today, today
	}

	switch cadence {
	case "weekly":
		offset := (int(date.Weekday()) + 6) % 7 // Monday starts the week.
		start := date.AddDate(0, 0, -offset)
		return start.Format(dateLayout), start.AddDate(0, 0, 6).Format(dateLayout)
	case "monthly":
		start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, loc)
		return start.Format(dateLayout), start.AddDate(0, 1, -1).Format(dateLayout)
	default:
		return today, today
	}
}
