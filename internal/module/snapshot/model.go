// Package snapshot serves the single response the browser caches offline.
package snapshot

import (
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/wiretime"
)

// Version is the export/import envelope version this API speaks.
const Version int64 = 6

// Response is the whole user dataset in one payload.
type Response struct {
	Version       int64                                     `json:"version"`
	Inbox         []inbox.Item                              `json:"inbox"`
	Tasks         []task.Task                               `json:"tasks"`
	Projects      []project.Project                         `json:"projects"`
	Notes         []note.Note                               `json:"notes"`
	Events        []event.Event                             `json:"events"`
	Areas         []area.Area                               `json:"areas"`
	Habits        []habit.Habit                             `json:"habits"`
	HabitCheckIns []habit.CheckIn                           `json:"habitCheckIns"`
	Profile       profile.Profile                           `json:"profile"`
	Counters      map[string]int32                          `json:"counters"`
	Integrations  map[string]settings.IntegrationConnection `json:"integrations"`
	AI            settings.AISettings                       `json:"ai"`
	ServerTime    wiretime.Timestamp                        `json:"serverTime"`
}
