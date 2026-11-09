package tasksync

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Repository is the only place task_external_links and provider credentials
// SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the task sync repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Link returns one task/provider link, or nil.
func (r *Repository) Link(
	ctx context.Context,
	userID, taskID, integrationID string,
) (*LinkRow, error) {
	var row LinkRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? and task_id = ? and integration_id = ?", userID, taskID, integrationID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// LinksForTask returns every provider link for one task.
func (r *Repository) LinksForTask(
	ctx context.Context,
	userID, taskID string,
) ([]LinkRow, error) {
	rows := make([]LinkRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and task_id = ?", userID, taskID).
		Order("integration_id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// Links returns every link for one user/provider, including tombstones.
func (r *Repository) Links(
	ctx context.Context,
	userID, integrationID string,
) ([]LinkRow, error) {
	rows := make([]LinkRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, integrationID).
		Order("id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// EnabledTaskIDs returns the task ids opted into one provider.
func (r *Repository) EnabledTaskIDs(
	ctx context.Context,
	userID, integrationID string,
) (map[string]bool, error) {
	rows := make([]LinkRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ? and enabled", userID, integrationID).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.TaskID != nil {
			ids[*row.TaskID] = true
		}
	}

	return ids, nil
}

// InsertLink stores a new opt-in mapping.
func (r *Repository) InsertLink(ctx context.Context, row LinkRow) error {
	return r.db.WithContext(ctx).Create(&row).Error
}

// SaveLink writes the mutable link columns back.
func (r *Repository) SaveLink(ctx context.Context, row LinkRow) error {
	return r.db.WithContext(ctx).Model(&LinkRow{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{
			"task_id":        row.TaskID,
			"external_id":    row.ExternalID,
			"external_ref":   row.ExternalRef,
			"enabled":        row.Enabled,
			"last_synced_at": row.LastSyncedAt,
			"last_error":     row.LastError,
		}).Error
}

// DeleteLink removes one mapping.
func (r *Repository) DeleteLink(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&LinkRow{}).Error
}

// Credential returns a provider credential, or nil.
func (r *Repository) Credential(
	ctx context.Context,
	userID, integrationID string,
) (*CredentialRow, error) {
	var row CredentialRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, integrationID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}

// SaveCredential inserts or updates a provider credential.
func (r *Repository) SaveCredential(ctx context.Context, row CredentialRow) error {
	return r.db.WithContext(ctx).Save(&row).Error
}

// DeleteCredential removes a provider credential.
func (r *Repository) DeleteCredential(ctx context.Context, userID, integrationID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, integrationID).
		Delete(&CredentialRow{}).Error
}

// CredentialUserIDs lists connected users for a provider.
func (r *Repository) CredentialUserIDs(ctx context.Context, integrationID string) ([]string, error) {
	userIDs := make([]string, 0)
	if err := r.db.WithContext(ctx).
		Model(&CredentialRow{}).
		Where("integration_id = ?", integrationID).
		Order("user_id asc").
		Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}

	return userIDs, nil
}
