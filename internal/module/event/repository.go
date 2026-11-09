package event

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place events SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the event repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// List returns every event in calendar order: date, start time (nulls first),
// then creation.
func (r *Repository) List(ctx context.Context, userID string) ([]Event, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("date asc, start_time asc nulls first, created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, row.ToWire())
	}

	return events, nil
}

// Get returns an event or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Event, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	event := row.ToWire()
	return &event, nil
}

// GetForUpdate locks the event row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Insert writes an event row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable event columns back.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"title":      row.Title,
			"date":       row.Date,
			"start_time": row.StartTime,
			"end_time":   row.EndTime,
			"notes":      row.Notes,
			"project_id": row.ProjectID,
			"updated_at": row.UpdatedAt,
		}).Error
}

// Delete removes an event.
func (r *Repository) Delete(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{}).Error
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
