package calendar

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Repository is the only place credential, state, and link SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the calendar repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Credential returns the stored credential for the user, or nil.
func (r *Repository) Credential(ctx context.Context, userID string) (*CredentialRow, error) {
	var row CredentialRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, IntegrationID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// SaveCredential inserts or updates the credential.
func (r *Repository) SaveCredential(ctx context.Context, row CredentialRow) error {
	return r.db.WithContext(ctx).Save(&row).Error
}

// DeleteCredential drops the user's Google credential.
func (r *Repository) DeleteCredential(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, IntegrationID).
		Delete(&CredentialRow{}).Error
}

// CredentialUserIDs lists connected credential owners for the background sync.
func (r *Repository) CredentialUserIDs(ctx context.Context) ([]string, error) {
	userIDs := make([]string, 0)
	if err := r.db.WithContext(ctx).
		Model(&CredentialRow{}).
		Where("integration_id = ?", IntegrationID).
		Order("user_id asc").
		Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}

	return userIDs, nil
}

// CreateState stores a one-time OAuth state.
func (r *Repository) CreateState(ctx context.Context, row OAuthStateRow) error {
	return r.db.WithContext(ctx).Create(&row).Error
}

// ConsumeState reads and deletes a state in one transaction.
func (r *Repository) ConsumeState(ctx context.Context, stateHash string) (*OAuthStateRow, error) {
	var row OAuthStateRow

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("state_hash = ?", stateHash).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		return tx.Where("state_hash = ?", stateHash).Delete(&OAuthStateRow{}).Error
	})
	if err != nil {
		return nil, err
	}
	if row.StateHash == "" {
		return nil, nil
	}

	return &row, nil
}

// PurgeExpiredStates removes abandoned OAuth attempts.
func (r *Repository) PurgeExpiredStates(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at < now()").Delete(&OAuthStateRow{}).Error
}

// Links lists every Google link for the user.
func (r *Repository) Links(ctx context.Context, userID string) ([]LinkRow, error) {
	rows := make([]LinkRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, IntegrationID).
		Order("id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// LinkByEvent returns the link for one local event, or nil.
func (r *Repository) LinkByEvent(ctx context.Context, userID, eventID string) (*LinkRow, error) {
	var row LinkRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ? and event_id = ?", userID, IntegrationID, eventID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// LinkByExternal returns the link for one Google event, or nil.
func (r *Repository) LinkByExternal(
	ctx context.Context,
	userID, calendarID, externalID string,
) (*LinkRow, error) {
	var row LinkRow
	err := r.db.WithContext(ctx).
		Where(
			"user_id = ? and integration_id = ? and calendar_id = ? and external_id = ?",
			userID, IntegrationID, calendarID, externalID,
		).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// InsertLink stores a new external mapping.
func (r *Repository) InsertLink(ctx context.Context, row LinkRow) error {
	return r.db.WithContext(ctx).Create(&row).Error
}

// SaveLink writes the mutable link columns back.
func (r *Repository) SaveLink(ctx context.Context, row LinkRow) error {
	return r.db.WithContext(ctx).Model(&LinkRow{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{
			"event_id":            row.EventID,
			"calendar_id":         row.CalendarID,
			"external_id":         row.ExternalID,
			"external_etag":       row.ExternalETag,
			"external_updated_at": row.ExternalUpdatedAt,
		}).Error
}

// DeleteLink removes one mapping.
func (r *Repository) DeleteLink(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&LinkRow{}).Error
}

// DeleteLinks drops every mapping for the user.
func (r *Repository) DeleteLinks(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, IntegrationID).
		Delete(&LinkRow{}).Error
}

// DetachCalendar keeps the local events but drops their Google links.
func (r *Repository) DetachCalendar(ctx context.Context, userID, calendarID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ? and calendar_id = ?", userID, IntegrationID, calendarID).
		Delete(&LinkRow{}).Error
}
