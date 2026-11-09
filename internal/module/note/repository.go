package note

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place notes SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the note repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// List returns every note ordered by creation.
func (r *Repository) List(ctx context.Context, userID string) ([]Note, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	notes := make([]Note, 0, len(rows))
	for _, row := range rows {
		notes = append(notes, row.ToWire())
	}

	return notes, nil
}

// Get returns a note or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Note, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	note := row.ToWire()
	return &note, nil
}

// GetForUpdate locks the note row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Insert writes a note row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable note columns back.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"title":      row.Title,
			"content":    row.Content,
			"tags":       row.Tags,
			"project_id": row.ProjectID,
			"area_id":    row.AreaID,
			"updated_at": row.UpdatedAt,
		}).Error
}

// Delete removes a note.
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
