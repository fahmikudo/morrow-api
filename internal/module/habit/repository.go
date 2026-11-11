package habit

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place habits and habit_check_ins SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the habit repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListActive returns the habits that are not archived.
func (r *Repository) ListActive(ctx context.Context, userID string) ([]Habit, error) {
	return r.list(ctx, userID, "archived_at is null")
}

// ListArchived returns the archived habits.
func (r *Repository) ListArchived(ctx context.Context, userID string) ([]Habit, error) {
	return r.list(ctx, userID, "archived_at is not null")
}

// ListAll returns every habit, archived included (snapshot and import paths).
func (r *Repository) ListAll(ctx context.Context, userID string) ([]Habit, error) {
	return r.list(ctx, userID, "")
}

// Get returns a habit or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Habit, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	habit := row.ToWire()
	return &habit, nil
}

// GetForUpdate locks the habit row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Exists reports whether the user owns the habit, archived or not.
func (r *Repository) Exists(ctx context.Context, tx *gorm.DB, userID, id string) (bool, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	var exists bool
	err := runner.WithContext(ctx).Raw(
		"select exists (select 1 from habits where id = ? and user_id = ?)", id, userID,
	).Scan(&exists).Error

	return exists, err
}

// Insert writes a habit row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable habit columns back.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"title":                 row.Title,
			"description":           row.Description,
			"area_id":               row.AreaID,
			"project_id":            row.ProjectID,
			"cadence":               row.Cadence,
			"target":                row.Target,
			"schedule_kind":         row.ScheduleKind,
			"weekdays":              row.Weekdays,
			"start_time":            row.StartTime,
			"end_time":              row.EndTime,
			"reminder_enabled":      row.ReminderEnabled,
			"reminder_lead_minutes": row.ReminderLeadMinutes,
			"start_date":            row.StartDate,
			"end_date":              row.EndDate,
			"archived_at":           row.ArchivedAt,
			"updated_at":            row.UpdatedAt,
		}).Error
}

// Delete removes a habit; its check-ins cascade.
func (r *Repository) Delete(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{}).Error
}

// ListCheckIns returns every check-in ordered by creation.
func (r *Repository) ListCheckIns(ctx context.Context, userID string) ([]CheckIn, error) {
	rows := make([]CheckInRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	checkIns := make([]CheckIn, 0, len(rows))
	for _, row := range rows {
		checkIns = append(checkIns, row.ToWire())
	}

	return checkIns, nil
}

// UpsertCheckIn keeps one row per habit per day and returns the stored row:
// a repeat returns the original, which makes the PUT idempotent.
func (r *Repository) UpsertCheckIn(
	ctx context.Context,
	tx *gorm.DB,
	userID, habitID string,
	date time.Time,
	id string,
	createdAt time.Time,
) (*CheckIn, error) {
	row := tx.WithContext(ctx).Raw(
		`insert into habit_check_ins (id, user_id, habit_id, date, created_at)
		 values (?, ?, ?, ?, ?)
		 on conflict (habit_id, date) do update set habit_id = excluded.habit_id
		 returning id, habit_id, date, created_at`,
		id, userID, habitID, date, createdAt,
	).Row()

	var stored CheckInRow
	if err := row.Scan(&stored.ID, &stored.HabitID, &stored.Date, &stored.CreatedAt); err != nil {
		return nil, err
	}

	checkIn := stored.ToWire()
	return &checkIn, nil
}

// DeleteCheckIn removes one day's check-in; a missing row is not an error.
func (r *Repository) DeleteCheckIn(ctx context.Context, tx *gorm.DB, userID, habitID string, date time.Time) error {
	return tx.WithContext(ctx).
		Where("habit_id = ? and user_id = ? and date = ?", habitID, userID, date).
		Delete(&CheckInRow{}).Error
}

func (r *Repository) list(ctx context.Context, userID, filter string) ([]Habit, error) {
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if filter != "" {
		query = query.Where(filter)
	}

	rows := make([]Row, 0)
	if err := query.Order("created_at asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	habits := make([]Habit, 0, len(rows))
	for _, row := range rows {
		habits = append(habits, row.ToWire())
	}

	return habits, nil
}

func (r *Repository) find(ctx context.Context, runner *gorm.DB, userID, id string, forUpdate bool) (*Row, error) {
	query := runner.WithContext(ctx).Where("id = ? and user_id = ?", id, userID)
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}

	var row Row
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}
