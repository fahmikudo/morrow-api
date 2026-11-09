package area

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place areas SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the area repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// List returns every area ordered by creation.
func (r *Repository) List(ctx context.Context, userID string) ([]Area, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	areas := make([]Area, 0, len(rows))
	for _, row := range rows {
		areas = append(areas, row.ToWire())
	}

	return areas, nil
}

// Get returns an area or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Area, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	area := row.ToWire()
	return &area, nil
}

// GetForUpdate locks the area row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Exists reports whether the user owns the area.
func (r *Repository) Exists(ctx context.Context, tx *gorm.DB, userID, id string) (bool, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	var exists bool
	err := runner.WithContext(ctx).Raw(
		"select exists (select 1 from areas where id = ? and user_id = ?)", id, userID,
	).Scan(&exists).Error

	return exists, err
}

// Insert writes an area row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable area columns back.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"name":        row.Name,
			"description": row.Description,
			"tone":        row.Tone,
			"icon":        row.Icon,
			"updated_at":  row.UpdatedAt,
		}).Error
}

// Delete removes an area; references are cleared by the foreign keys.
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
