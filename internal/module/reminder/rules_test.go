package reminder

import (
	"testing"
	"time"

	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/wiretime"
)

func testDate(t *testing.T, raw string) wiretime.Date {
	t.Helper()

	parsed, err := wiretime.ParseDate(raw)
	if err != nil {
		t.Fatalf("parse date %q: %v", raw, err)
	}

	return parsed
}

func testClock(t *testing.T, raw string, loc *time.Location) time.Time {
	t.Helper()

	parsed, err := time.ParseInLocation("2006-01-02 15:04", raw, loc)
	if err != nil {
		t.Fatalf("parse clock %q: %v", raw, err)
	}

	return parsed
}

func testLocation(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	return loc
}

func dueTask(id, key, title, due string, status string, t *testing.T) task.Task {
	value := testDate(t, due)
	return task.Task{ID: id, Key: key, Title: title, Status: status, DueDate: &value}
}

func TestComputeDueTaskRules(t *testing.T) {
	loc := testLocation(t)
	settings := Settings{Enabled: true, TaskTime: "09:00", EventLeadMinutes: 15}

	cases := []struct {
		name string
		now  string
		task task.Task
		want int
		kind string
		text string
	}{
		{
			name: "due today fires at the task time",
			now:  "2026-11-09 09:00",
			task: dueTask("t1", "SB-1", "Ship it", "2026-11-09", "todo", t),
			want: 1,
			kind: "Task due today",
			text: "SB-1 · Ship it",
		},
		{
			name: "due today does not fire before the task time",
			now:  "2026-11-09 08:59",
			task: dueTask("t1", "SB-1", "Ship it", "2026-11-09", "todo", t),
			want: 0,
		},
		{
			name: "overdue task is titled overdue",
			now:  "2026-11-09 09:30",
			task: dueTask("t1", "SB-1", "Ship it", "2026-11-08", "todo", t),
			want: 1,
			kind: "Overdue task",
			text: "SB-1 · Ship it",
		},
		{
			name: "done task is ignored",
			now:  "2026-11-09 09:30",
			task: dueTask("t1", "SB-1", "Ship it", "2026-11-09", "done", t),
			want: 0,
		},
		{
			name: "future task is ignored",
			now:  "2026-11-09 09:30",
			task: dueTask("t1", "SB-1", "Ship it", "2026-11-10", "todo", t),
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := testClock(t, tc.now, loc)
			due := computeDue(now, loc, settings, ruleData{Tasks: []task.Task{tc.task}})

			if len(due) != tc.want {
				t.Fatalf("candidates = %d, want %d (%v)", len(due), tc.want, due)
			}
			if tc.want == 0 {
				return
			}

			if due[0].Kind != "task" || due[0].Title != tc.kind || due[0].Body != tc.text {
				t.Errorf("candidate = %+v, want kind %q title %q body %q", due[0], "task", tc.kind, tc.text)
			}
			if due[0].RefID != tc.task.ID || due[0].Href != "/tasks" {
				t.Errorf("candidate ref/href = %q %q", due[0].RefID, due[0].Href)
			}
		})
	}
}

func TestComputeDueEventRules(t *testing.T) {
	loc := testLocation(t)
	start := "09:40"
	settings := Settings{Enabled: true, TaskTime: "09:00", EventLeadMinutes: 15}
	item := event.Event{ID: "e1", Title: "Standup", Date: testDate(t, "2026-11-09"), StartTime: &start}

	cases := []struct {
		name string
		now  string
		want int
		lead int32
		text string
	}{
		{name: "exactly at the lead window", now: "2026-11-09 09:25", want: 1, lead: 15, text: "09:40 · Standup"},
		{name: "inside the lead window", now: "2026-11-09 09:30", want: 1, lead: 15, text: "09:40 · Standup"},
		{name: "after the catch-up window", now: "2026-11-09 09:41", want: 0, lead: 15},
		{name: "at start has its own title", now: "2026-11-09 09:40", want: 1, lead: 0, text: "09:40 · Standup"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings.EventLeadMinutes = tc.lead
			now := testClock(t, tc.now, loc)
			due := computeDue(now, loc, settings, ruleData{Events: []event.Event{item}})

			if len(due) != tc.want {
				t.Fatalf("candidates = %d, want %d (%v)", len(due), tc.want, due)
			}
			if tc.want == 0 {
				return
			}

			if due[0].Kind != "event" || due[0].Href != "/calendar" || due[0].Body != tc.text {
				t.Errorf("candidate = %+v", due[0])
			}
		})
	}
}

func TestComputeDueEventRequiresStartTime(t *testing.T) {
	loc := testLocation(t)
	settings := Settings{Enabled: true, TaskTime: "09:00", EventLeadMinutes: 15}
	item := event.Event{ID: "e1", Title: "All day", Date: testDate(t, "2026-11-09")}

	due := computeDue(testClock(t, "2026-11-09 09:30", loc), loc, settings, ruleData{
		Events: []event.Event{item},
	})
	if len(due) != 0 {
		t.Fatalf("candidates = %v, want none", due)
	}
}

func TestComputeDueHabitRules(t *testing.T) {
	loc := testLocation(t)
	start := "07:00"
	settings := Settings{Enabled: true, TaskTime: "09:00", EventLeadMinutes: 15}

	base := habit.Habit{
		ID:        "h1",
		Title:     "Morning run",
		Cadence:   "daily",
		Target:    1,
		Schedule:  habit.Schedule{Kind: "fixed", Weekdays: []int32{1}},
		StartTime: &start,
		StartDate: testDate(t, "2026-11-01"),
	}

	t.Run("fixed weekday fires on that weekday", func(t *testing.T) {
		due := computeDue(testClock(t, "2026-11-09 07:30", loc), loc, settings, ruleData{
			Habits: []habit.Habit{base},
		})
		if len(due) != 1 || due[0].Kind != "habit" || due[0].Href != "/habits" ||
			due[0].Body != "07:00 · Morning run" {
			t.Fatalf("candidates = %+v, want one habit reminder", due)
		}
	})

	t.Run("fixed weekday skips other days", func(t *testing.T) {
		due := computeDue(testClock(t, "2026-11-10 07:30", loc), loc, settings, ruleData{
			Habits: []habit.Habit{base},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none", due)
		}
	})

	t.Run("today check-in skips the reminder", func(t *testing.T) {
		due := computeDue(testClock(t, "2026-11-09 07:30", loc), loc, settings, ruleData{
			Habits:   []habit.Habit{base},
			CheckIns: []habit.CheckIn{{HabitID: "h1", Date: testDate(t, "2026-11-09")}},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none", due)
		}
	})

	t.Run("met weekly target skips the reminder", func(t *testing.T) {
		weekly := base
		weekly.Schedule = habit.Schedule{Kind: "flexible"}
		weekly.Cadence = "weekly"
		weekly.Target = 2

		due := computeDue(testClock(t, "2026-11-12 07:30", loc), loc, settings, ruleData{
			Habits: []habit.Habit{weekly},
			CheckIns: []habit.CheckIn{
				{HabitID: "h1", Date: testDate(t, "2026-11-09")},
				{HabitID: "h1", Date: testDate(t, "2026-11-10")},
			},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none", due)
		}
	})

	t.Run("explicitly disabled habit is skipped", func(t *testing.T) {
		off := base
		disabled := false
		off.ReminderEnabled = &disabled

		due := computeDue(testClock(t, "2026-11-09 07:30", loc), loc, settings, ruleData{
			Habits: []habit.Habit{off},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none", due)
		}
	})

	t.Run("reminder lead fires before the start time", func(t *testing.T) {
		lead := int32(15)
		early := base
		early.ReminderLeadMinutes = &lead

		due := computeDue(testClock(t, "2026-11-09 06:45", loc), loc, settings, ruleData{
			Habits: []habit.Habit{early},
		})
		if len(due) != 1 || !due[0].At.Equal(testClock(t, "2026-11-09 06:45", loc)) {
			t.Fatalf("candidates = %+v, want one reminder at 06:45", due)
		}

		due = computeDue(testClock(t, "2026-11-09 06:44", loc), loc, settings, ruleData{
			Habits: []habit.Habit{early},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none before the lead", due)
		}
	})

	t.Run("habit without a start time is ignored", func(t *testing.T) {
		noTime := base
		noTime.StartTime = nil

		due := computeDue(testClock(t, "2026-11-09 07:30", loc), loc, settings, ruleData{
			Habits: []habit.Habit{noTime},
		})
		if len(due) != 0 {
			t.Fatalf("candidates = %v, want none", due)
		}
	})
}

func TestComputeDueOrderAndEmpty(t *testing.T) {
	loc := testLocation(t)
	settings := Settings{Enabled: true, TaskTime: "09:00", EventLeadMinutes: 0}
	start := "09:00"

	data := ruleData{
		Tasks:  []task.Task{dueTask("t1", "SB-1", "Ship it", "2026-11-09", "todo", t)},
		Events: []event.Event{{ID: "e1", Title: "Standup", Date: testDate(t, "2026-11-09"), StartTime: &start}},
		Habits: []habit.Habit{{
			ID: "h1", Title: "Run", Cadence: "daily", Target: 1,
			Schedule:  habit.Schedule{Kind: "flexible"},
			StartTime: &start,
			StartDate: testDate(t, "2026-11-01"),
		}},
	}

	due := computeDue(testClock(t, "2026-11-09 09:00", loc), loc, settings, data)
	if len(due) != 3 {
		t.Fatalf("candidates = %d, want 3", len(due))
	}

	want := []string{"task", "event", "habit"}
	for index, kind := range want {
		if due[index].Kind != kind {
			t.Errorf("candidate %d kind = %q, want %q", index, due[index].Kind, kind)
		}
	}

	empty := computeDue(testClock(t, "2026-11-09 09:00", loc), loc, settings, ruleData{})
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty result = %v, want non-nil empty slice", empty)
	}
}
