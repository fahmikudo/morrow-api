// Package importer replaces a user's dataset from the v6 export envelope.
package importer

import (
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/task"
)

// Database is the v6 export envelope, identical to the frontend's `Database`.
type Database struct {
	Version       int64             `json:"version"`
	Inbox         []inbox.Item      `json:"inbox"`
	Tasks         []task.Task       `json:"tasks"`
	Projects      []project.Project `json:"projects"`
	Notes         []note.Note       `json:"notes"`
	Events        []event.Event     `json:"events"`
	Areas         []area.Area       `json:"areas"`
	Habits        []habit.Habit     `json:"habits"`
	HabitCheckIns []habit.CheckIn   `json:"habitCheckIns"`
	Profile       profile.Profile   `json:"profile"`
	Counters      map[string]int32  `json:"counters"`
}
