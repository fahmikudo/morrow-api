package importer

import (
	"context"

	"gorm.io/gorm"
)

// Repository clears the user's mutable dataset.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the importer repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Clear deletes the user's data in foreign-key-safe order.
func (r *Repository) Clear(ctx context.Context, tx *gorm.DB, userID string) error {
	for _, table := range []string{
		"habit_check_ins",
		"habit_skips",
		"tasks",
		"notes",
		"events",
		"habits",
		"inbox_items",
		"projects",
		"areas",
		"counters",
	} {
		if err := tx.WithContext(ctx).Exec("delete from "+table+" where user_id = ?", userID).Error; err != nil {
			return err
		}
	}

	return nil
}
