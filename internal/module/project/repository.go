package project

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place projects SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the project repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// List returns every project ordered by creation.
func (r *Repository) List(ctx context.Context, userID string) ([]Project, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	projects := make([]Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, row.ToWire())
	}

	return projects, nil
}

// Get returns a project or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Project, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	project := row.ToWire()
	return &project, nil
}

// GetForUpdate locks the project row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Exists reports whether the user owns the project.
func (r *Repository) Exists(ctx context.Context, tx *gorm.DB, userID, id string) (bool, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	var exists bool
	err := runner.WithContext(ctx).Raw(
		"select exists (select 1 from projects where id = ? and user_id = ?)", id, userID,
	).Scan(&exists).Error

	return exists, err
}

// Key returns the project's immutable key, or nil when it does not exist.
func (r *Repository) Key(ctx context.Context, tx *gorm.DB, userID, id string) (*string, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	var key string
	result := runner.WithContext(ctx).Raw(
		`select "key" from projects where id = ? and user_id = ?`, id, userID,
	).Scan(&key)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return &key, nil
}

// TakenKeys returns every key the user already uses.
func (r *Repository) TakenKeys(ctx context.Context, tx *gorm.DB, userID string) (map[string]struct{}, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	keys := make([]string, 0)
	if err := runner.WithContext(ctx).Model(&Row{}).
		Where("user_id = ?", userID).
		Pluck("key", &keys).Error; err != nil {
		return nil, err
	}

	taken := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		taken[key] = struct{}{}
	}

	return taken, nil
}

// InsertIfKeyFree inserts only when the (user_id, key) pair is free.
func (r *Repository) InsertIfKeyFree(ctx context.Context, tx *gorm.DB, row Row) (bool, error) {
	result := tx.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row)

	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

// Insert writes a project row without conflict handling (the importer path).
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable project columns back; `key` never changes.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"name":        row.Name,
			"description": row.Description,
			"status":      row.Status,
			"area_id":     row.AreaID,
			"updated_at":  row.UpdatedAt,
		}).Error
}

// Delete removes a project; references are cleared by the foreign keys.
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
