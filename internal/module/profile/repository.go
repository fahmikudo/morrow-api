package profile

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place profile SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds a profile repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Get reads the profile, returning nil when the user has none.
func (r *Repository) Get(ctx context.Context, userID string) (*Profile, error) {
	var row Row
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	profile := row.ToWire()
	return &profile, nil
}

// GetForUpdate locks the profile row for the duration of the transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID string) (*Row, error) {
	var row Row
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// Insert writes the profile row for a user.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, userID string, profile Profile) error {
	row := Row{UserID: userID}
	row.Load(profile)

	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes every profile column back.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("user_id = ?", row.UserID).
		Updates(map[string]any{
			"name":       row.Name,
			"email":      row.Email,
			"role":       row.Role,
			"timezone":   row.Timezone,
			"joined_at":  row.JoinedAt,
			"updated_at": row.UpdatedAt,
		}).Error
}
