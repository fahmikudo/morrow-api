// Package habit owns habits, their schedules, and the daily check-ins.
package habit

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/pgarray"
	"morrow-api/internal/platform/wiretime"
)

// Schedule is the tagged union stored as `schedule_kind` + `weekdays`.
type Schedule struct {
	Kind     string
	Weekdays []int32
}

// MarshalJSON emits `{"kind":"flexible"}` or `{"kind":"fixed","weekdays":[…]}`.
func (s Schedule) MarshalJSON() ([]byte, error) {
	if s.Kind == "fixed" {
		weekdays := s.Weekdays
		if weekdays == nil {
			weekdays = []int32{}
		}

		return json.Marshal(struct {
			Kind     string  `json:"kind"`
			Weekdays []int32 `json:"weekdays"`
		}{Kind: s.Kind, Weekdays: weekdays})
	}

	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{Kind: "flexible"})
}

// UnmarshalJSON rejects an unknown kind and a fixed schedule without weekdays.
func (s *Schedule) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind     string   `json:"kind"`
		Weekdays *[]int32 `json:"weekdays"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch raw.Kind {
	case "flexible":
		*s = Schedule{Kind: "flexible"}
		return nil
	case "fixed":
		if raw.Weekdays == nil {
			return errors.New("missing field weekdays")
		}
		*s = Schedule{Kind: "fixed", Weekdays: *raw.Weekdays}
		return nil
	default:
		return fmt.Errorf("unknown variant %q, expected flexible or fixed", raw.Kind)
	}
}

// Habit is the API shape of a habit.
type Habit struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description *string  `json:"description,omitempty"`
	AreaID      *string  `json:"areaId,omitempty"`
	ProjectID   *string  `json:"projectId,omitempty"`
	Cadence     string   `json:"cadence"`
	Target      int32    `json:"target"`
	Schedule    Schedule `json:"schedule"`
	StartTime   *string  `json:"startTime,omitempty"`
	EndTime     *string  `json:"endTime,omitempty"`
	// ReminderEnabled nil means the global reminder setting decides; false
	// suppresses this habit's reminder.
	ReminderEnabled *bool `json:"reminderEnabled,omitempty"`
	// ReminderLeadMinutes nil means fire at StartTime; otherwise the reminder
	// moves this many minutes earlier.
	ReminderLeadMinutes *int32        `json:"reminderLeadMinutes,omitempty"`
	StartDate           wiretime.Date `json:"startDate"`
	// EndDate is the last day the habit counts, inclusive. Nil means ongoing.
	EndDate    *wiretime.Date      `json:"endDate,omitempty"`
	ArchivedAt *wiretime.Timestamp `json:"archivedAt,omitempty"`
	CreatedAt  wiretime.Timestamp  `json:"createdAt"`
	UpdatedAt  wiretime.Timestamp  `json:"updatedAt"`
}

// Row is the persisted shape of a habit.
type Row struct {
	ID                  string           `gorm:"column:id;primaryKey"`
	UserID              string           `gorm:"column:user_id"`
	Title               string           `gorm:"column:title"`
	Description         *string          `gorm:"column:description"`
	AreaID              *string          `gorm:"column:area_id"`
	ProjectID           *string          `gorm:"column:project_id"`
	Cadence             string           `gorm:"column:cadence"`
	Target              int32            `gorm:"column:target"`
	ScheduleKind        string           `gorm:"column:schedule_kind"`
	Weekdays            pgarray.IntArray `gorm:"column:weekdays"`
	StartTime           *wiretime.Clock  `gorm:"column:start_time"`
	EndTime             *wiretime.Clock  `gorm:"column:end_time"`
	ReminderEnabled     *bool            `gorm:"column:reminder_enabled"`
	ReminderLeadMinutes *int32           `gorm:"column:reminder_lead_minutes"`
	StartDate           time.Time        `gorm:"column:start_date"`
	EndDate             *time.Time       `gorm:"column:end_date"`
	ArchivedAt          *time.Time       `gorm:"column:archived_at"`
	CreatedAt           time.Time        `gorm:"column:created_at"`
	UpdatedAt           time.Time        `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (Row) TableName() string {
	return "habits"
}

// ToWire converts a stored row into the API shape.
func (r Row) ToWire() Habit {
	schedule := Schedule{Kind: "flexible"}
	if r.ScheduleKind == "fixed" {
		schedule = Schedule{Kind: "fixed", Weekdays: r.Weekdays.Values()}
	}

	var archivedAt *wiretime.Timestamp
	if r.ArchivedAt != nil {
		value := wiretime.Timestamp(*r.ArchivedAt)
		archivedAt = &value
	}

	var endDate *wiretime.Date
	if r.EndDate != nil {
		value := wiretime.Date(*r.EndDate)
		endDate = &value
	}

	return Habit{
		ID:                  r.ID,
		Title:               r.Title,
		Description:         r.Description,
		AreaID:              r.AreaID,
		ProjectID:           r.ProjectID,
		Cadence:             r.Cadence,
		Target:              r.Target,
		Schedule:            schedule,
		StartTime:           wiretime.ClockText(r.StartTime),
		EndTime:             wiretime.ClockText(r.EndTime),
		ReminderEnabled:     r.ReminderEnabled,
		ReminderLeadMinutes: r.ReminderLeadMinutes,
		StartDate:           wiretime.Date(r.StartDate),
		EndDate:             endDate,
		ArchivedAt:          archivedAt,
		CreatedAt:           wiretime.Timestamp(r.CreatedAt),
		UpdatedAt:           wiretime.Timestamp(r.UpdatedAt),
	}
}

// Load overwrites the stored fields from the API shape.
func (r *Row) Load(h Habit) {
	r.ID = h.ID
	r.Title = h.Title
	r.Description = h.Description
	r.AreaID = h.AreaID
	r.ProjectID = h.ProjectID
	r.Cadence = h.Cadence
	r.Target = h.Target
	r.StartTime = wiretime.ClockValue(h.StartTime)
	r.EndTime = wiretime.ClockValue(h.EndTime)
	r.ReminderEnabled = h.ReminderEnabled
	r.ReminderLeadMinutes = h.ReminderLeadMinutes
	r.StartDate = h.StartDate.Time()
	if h.EndDate != nil {
		value := h.EndDate.Time()
		r.EndDate = &value
	} else {
		r.EndDate = nil
	}
	r.CreatedAt = h.CreatedAt.Time()
	r.UpdatedAt = h.UpdatedAt.Time()

	if h.ArchivedAt != nil {
		value := h.ArchivedAt.Time()
		r.ArchivedAt = &value
	} else {
		r.ArchivedAt = nil
	}

	if h.Schedule.Kind == "fixed" {
		r.ScheduleKind = "fixed"
		r.Weekdays = pgarray.Ints(h.Schedule.Weekdays)
	} else {
		r.ScheduleKind = "flexible"
		r.Weekdays = pgarray.IntArray{}
	}
}

// CheckIn is the API shape of a habit check-in.
type CheckIn struct {
	ID        string             `json:"id"`
	HabitID   string             `json:"habitId"`
	Date      wiretime.Date      `json:"date"`
	CreatedAt wiretime.Timestamp `json:"createdAt"`
}

// CheckInRow is the persisted shape of a check-in.
type CheckInRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	HabitID   string    `gorm:"column:habit_id"`
	Date      time.Time `gorm:"column:date"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName keeps the Gorm mapping explicit.
func (CheckInRow) TableName() string {
	return "habit_check_ins"
}

// ToWire converts a stored row into the API shape.
func (r CheckInRow) ToWire() CheckIn {
	return CheckIn{
		ID:        r.ID,
		HabitID:   r.HabitID,
		Date:      wiretime.Date(r.Date),
		CreatedAt: wiretime.Timestamp(r.CreatedAt),
	}
}

// Skip is the API shape of an explicit "not today".
type Skip struct {
	ID        string             `json:"id"`
	HabitID   string             `json:"habitId"`
	Date      wiretime.Date      `json:"date"`
	CreatedAt wiretime.Timestamp `json:"createdAt"`
}

// SkipRow is the persisted shape of a skip.
type SkipRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	HabitID   string    `gorm:"column:habit_id"`
	Date      time.Time `gorm:"column:date"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName keeps the Gorm mapping explicit.
func (SkipRow) TableName() string {
	return "habit_skips"
}

// ToWire converts a stored row into the API shape.
func (r SkipRow) ToWire() Skip {
	return Skip{
		ID:        r.ID,
		HabitID:   r.HabitID,
		Date:      wiretime.Date(r.Date),
		CreatedAt: wiretime.Timestamp(r.CreatedAt),
	}
}

// New is the create body.
type New struct {
	Title               string    `json:"title"`
	Description         *string   `json:"description"`
	AreaID              *string   `json:"areaId"`
	ProjectID           *string   `json:"projectId"`
	Cadence             *string   `json:"cadence"`
	Target              *int32    `json:"target"`
	Schedule            *Schedule `json:"schedule"`
	StartTime           *string   `json:"startTime"`
	EndTime             *string   `json:"endTime"`
	ReminderEnabled     *bool     `json:"reminderEnabled"`
	ReminderLeadMinutes *int32    `json:"reminderLeadMinutes"`
	StartDate           *string   `json:"startDate"`
	EndDate             *string   `json:"endDate"`
}

// Update is the PATCH body; five fields accept an explicit null.
type Update struct {
	Title               *string           `json:"title"`
	Description         *string           `json:"description"`
	Cadence             *string           `json:"cadence"`
	Target              *float64          `json:"target"`
	Schedule            *Schedule         `json:"schedule"`
	StartDate           *string           `json:"startDate"`
	AreaID              httpx.Opt[string] `json:"areaId"`
	ProjectID           httpx.Opt[string] `json:"projectId"`
	ArchivedAt          httpx.Opt[string] `json:"archivedAt"`
	StartTime           httpx.Opt[string] `json:"startTime"`
	EndTime             httpx.Opt[string] `json:"endTime"`
	ReminderEnabled     httpx.Opt[bool]   `json:"reminderEnabled"`
	ReminderLeadMinutes httpx.Opt[int32]  `json:"reminderLeadMinutes"`
	EndDate             httpx.Opt[string] `json:"endDate"`
}

// UpdateOptions is the body policy for PATCH /habits.
var UpdateOptions = httpx.Options{
	Nullable: []string{
		"areaId", "projectId", "archivedAt", "startTime", "endTime",
		"reminderEnabled", "reminderLeadMinutes", "endDate",
	},
	Strict: true,
}
