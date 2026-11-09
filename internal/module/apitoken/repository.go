package apitoken

import (
	"context"

	"gorm.io/gorm"
)

// Repository is the only place api_tokens SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the token repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create stores a token row inside the caller's transaction.
func (r *Repository) Create(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// List returns the user's tokens, newest first.
func (r *Repository) List(ctx context.Context, userID string) ([]APIToken, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at desc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	tokens := make([]APIToken, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, row.ToWire())
	}

	return tokens, nil
}

// Delete revokes one token and reports whether a row was removed.
func (r *Repository) Delete(ctx context.Context, userID, id string) (bool, error) {
	result := r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{})
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}
