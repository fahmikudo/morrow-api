package reminder

import (
	"context"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/platform/ids"
)

// Repository is the only place reminder_settings, push_subscriptions, and
// reminder_deliveries SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the reminder repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) runner(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// EnsureDefaults creates the reminder settings row a user needs. It is
// idempotent and safe on every read.
func (r *Repository) EnsureDefaults(ctx context.Context, tx *gorm.DB, userID string) error {
	return r.runner(tx).WithContext(ctx).Exec(
		"insert into reminder_settings (user_id) values (?) on conflict do nothing", userID,
	).Error
}

// Settings returns the reminder settings, creating the default row if needed.
func (r *Repository) Settings(ctx context.Context, tx *gorm.DB, userID string) (*SettingsRow, error) {
	if err := r.EnsureDefaults(ctx, tx, userID); err != nil {
		return nil, err
	}

	var row SettingsRow
	if err := r.runner(tx).WithContext(ctx).Where("user_id = ?", userID).Take(&row).Error; err != nil {
		return nil, err
	}

	return &row, nil
}

// SaveSettings writes the mutable reminder settings back.
func (r *Repository) SaveSettings(ctx context.Context, tx *gorm.DB, row SettingsRow) error {
	return r.runner(tx).WithContext(ctx).Model(&SettingsRow{}).
		Where("user_id = ?", row.UserID).
		Updates(map[string]any{
			"enabled":            row.Enabled,
			"task_time":          row.TaskTime,
			"event_lead_minutes": row.EventLeadMinutes,
			"updated_at":         row.UpdatedAt,
		}).Error
}

// UpsertSubscription stores a device subscription; a repeat rebinds the
// endpoint to the user and refreshes its keys.
func (r *Repository) UpsertSubscription(
	ctx context.Context,
	userID, endpoint, p256dh, auth string,
	userAgent *string,
	now time.Time,
) error {
	return r.db.WithContext(ctx).Exec(
		`insert into push_subscriptions
			(id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_used_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?)
		 on conflict (endpoint) do update set
			user_id = excluded.user_id,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent,
			last_used_at = excluded.last_used_at`,
		ids.New("push"), userID, endpoint, p256dh, auth, userAgent, now, now,
	).Error
}

// Subscriptions returns every device subscription the user registered.
func (r *Repository) Subscriptions(ctx context.Context, userID string) ([]SubscriptionRow, error) {
	rows := make([]SubscriptionRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// DeleteSubscription removes one subscription by id.
func (r *Repository) DeleteSubscription(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&SubscriptionRow{}).Error
}

// DeleteSubscriptionByEndpoint removes a subscription scoped to its owner.
func (r *Repository) DeleteSubscriptionByEndpoint(ctx context.Context, userID, endpoint string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and endpoint = ?", userID, endpoint).
		Delete(&SubscriptionRow{}).Error
}

// ClaimDelivery records a delivery unless the same item was already delivered
// for that day, reporting whether this call won the race.
func (r *Repository) ClaimDelivery(ctx context.Context, tx *gorm.DB, row DeliveryRow) (bool, error) {
	result := r.runner(tx).WithContext(ctx).Exec(
		`insert into reminder_deliveries
			(id, user_id, kind, ref_id, occurrence_date, title, body, href, sent_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 on conflict (user_id, kind, ref_id, occurrence_date) do nothing`,
		row.ID, row.UserID, row.Kind, row.RefID, row.OccurrenceDate,
		row.Title, row.Body, row.Href, row.SentAt,
	)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

// Today returns the user's undelivered reminders for one calendar date.
func (r *Repository) Today(ctx context.Context, userID, date string) ([]DeliveryRow, error) {
	rows := make([]DeliveryRow, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and occurrence_date = ? and dismissed_at is null", userID, date).
		Order("sent_at desc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// Dismiss hides one reminder for the user, reporting whether it changed a row.
func (r *Repository) Dismiss(ctx context.Context, userID, id string, now time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&DeliveryRow{}).
		Where("id = ? and user_id = ? and dismissed_at is null", id, userID).
		Update("dismissed_at", now)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

// EnabledUserIDs lists every account whose reminders are switched on.
func (r *Repository) EnabledUserIDs(ctx context.Context) ([]string, error) {
	userIDs := make([]string, 0)
	if err := r.db.WithContext(ctx).
		Model(&SettingsRow{}).
		Where("enabled").
		Order("user_id asc").
		Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}

	return userIDs, nil
}
