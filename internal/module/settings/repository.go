package settings

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Repository is the only place integrations and ai_settings SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the settings repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// EnsureDefaults creates the catalogue rows a user needs. It is idempotent and
// safe on every read.
func (r *Repository) EnsureDefaults(ctx context.Context, tx *gorm.DB, userID string) error {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	for _, integrationID := range IntegrationIDs {
		err := runner.WithContext(ctx).Exec(
			`insert into integrations (user_id, integration_id) values (?, ?)
			 on conflict (user_id, integration_id) do nothing`,
			userID, integrationID,
		).Error
		if err != nil {
			return err
		}
	}

	return runner.WithContext(ctx).Exec(
		"insert into ai_settings (user_id) values (?) on conflict do nothing", userID,
	).Error
}

// Integrations returns every integration for the user, keyed by id.
func (r *Repository) Integrations(ctx context.Context, userID string) (map[string]IntegrationConnection, error) {
	if err := r.EnsureDefaults(ctx, nil, userID); err != nil {
		return nil, err
	}

	rows := make([]IntegrationRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and integration_id in ?", userID, IntegrationIDs).
		Order("integration_id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	integrations := make(map[string]IntegrationConnection, len(rows))
	for _, row := range rows {
		integrations[row.IntegrationID] = row.ToWire()
	}

	return integrations, nil
}

// Integration returns one integration row, or nil.
func (r *Repository) Integration(ctx context.Context, tx *gorm.DB, userID, integrationID string) (*IntegrationConnection, error) {
	if err := r.EnsureDefaults(ctx, tx, userID); err != nil {
		return nil, err
	}

	runner := r.db
	if tx != nil {
		runner = tx
	}

	var row IntegrationRow
	err := runner.WithContext(ctx).
		Where("user_id = ? and integration_id = ?", userID, integrationID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	connection := row.ToWire()
	return &connection, nil
}

// SetIntegration writes the toggle state back.
func (r *Repository) SetIntegration(ctx context.Context, tx *gorm.DB, userID, integrationID string, connection IntegrationConnection) error {
	var lastSyncAt *time.Time
	if connection.LastSyncAt != nil {
		value := connection.LastSyncAt.Time()
		lastSyncAt = &value
	}

	return tx.WithContext(ctx).Model(&IntegrationRow{}).
		Where("user_id = ? and integration_id = ?", userID, integrationID).
		Updates(map[string]any{
			"enabled":       connection.Enabled,
			"connected":     connection.Connected,
			"last_sync_at":  lastSyncAt,
			"account_email": connection.AccountEmail,
			"calendar_id":   connection.CalendarID,
			"last_error":    connection.LastError,
		}).Error
}

// AIRecord returns the assistant settings, creating the default row if needed.
func (r *Repository) AIRecord(ctx context.Context, tx *gorm.DB, userID string) (*AIRow, error) {
	if err := r.EnsureDefaults(ctx, tx, userID); err != nil {
		return nil, err
	}

	runner := r.db
	if tx != nil {
		runner = tx
	}

	var row AIRow
	if err := runner.WithContext(ctx).Where("user_id = ?", userID).Take(&row).Error; err != nil {
		return nil, err
	}

	return &row, nil
}

// SetAIRecord writes the assistant settings back.
func (r *Repository) SetAIRecord(ctx context.Context, tx *gorm.DB, record AIRow) error {
	return tx.WithContext(ctx).Model(&AIRow{}).
		Where("user_id = ?", record.UserID).
		Updates(map[string]any{
			"enabled":          record.Enabled,
			"provider":         record.Provider,
			"model":            record.Model,
			"reasoning_effort": record.ReasoningEffort,
			"api_key":          record.APIKey,
			"status":           record.Status,
			"last_tested_at":   record.LastTestedAt,
		}).Error
}
