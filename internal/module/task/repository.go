package task

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place tasks SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the task repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// List returns every task ordered by creation.
func (r *Repository) List(ctx context.Context, userID string) ([]Task, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	tasks := make([]Task, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, row.ToWire())
	}

	return tasks, nil
}

// Get returns a task or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Task, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	task := row.ToWire()
	return &task, nil
}

// GetForUpdate locks the task row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Task, error) {
	row, err := r.find(ctx, tx, userID, id, true)
	if err != nil || row == nil {
		return nil, err
	}

	task := row.ToWire()
	return &task, nil
}

// Insert writes a task row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable task columns back; `key` and `created_at` never change.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"title":       row.Title,
			"description": row.Description,
			"type":        row.Type,
			"status":      row.Status,
			"priority":    row.Priority,
			"due_date":    row.DueDate,
			"start_time":  row.StartTime,
			"end_time":    row.EndTime,
			"project_id":  row.ProjectID,
			"area_id":     row.AreaID,
			"parent_id":   row.ParentID,
			"labels":      row.Labels,
			"updated_at":  row.UpdatedAt,
		}).Error
}

// Delete removes a task; children detach through the foreign key.
func (r *Repository) Delete(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{}).Error
}

// KeyTaken reports whether the user already uses the task key.
func (r *Repository) KeyTaken(ctx context.Context, tx *gorm.DB, userID, key string) (bool, error) {
	var exists bool
	err := tx.WithContext(ctx).Raw(
		`select exists (select 1 from tasks where user_id = ? and "key" = ?)`, userID, key,
	).Scan(&exists).Error

	return exists, err
}

// HasChildren reports whether any task points at id.
func (r *Repository) HasChildren(ctx context.Context, tx *gorm.DB, id string) (bool, error) {
	var exists bool
	err := tx.WithContext(ctx).Raw(
		"select exists (select 1 from tasks where parent_id = ? and id <> ?)", id, id,
	).Scan(&exists).Error

	return exists, err
}

// SetParent rewrites only parent_id; the importer uses it for its second pass.
func (r *Repository) SetParent(ctx context.Context, tx *gorm.DB, userID, id string, parentID *string) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", id, userID).
		Update("parent_id", parentID).Error
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
