// Package reminder owns the reminder rules, their deliveries, and the Web Push
// subscriptions devices register.
package reminder

import (
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// Settings is the API shape of the reminder settings.
type Settings struct {
	Enabled          bool   `json:"enabled"`
	TaskTime         string `json:"taskTime"`
	EventLeadMinutes int32  `json:"eventLeadMinutes"`
}

// SettingsUpdate is the PATCH body for the reminder settings.
type SettingsUpdate struct {
	Enabled          *bool   `json:"enabled"`
	TaskTime         *string `json:"taskTime"`
	EventLeadMinutes *int32  `json:"eventLeadMinutes"`
}

// SettingsUpdateOptions is the body policy for PATCH /reminders/settings.
var SettingsUpdateOptions = httpx.Options{Strict: true}

// Reminder is one delivered reminder.
type Reminder struct {
	ID             string              `json:"id"`
	Kind           string              `json:"kind"`
	RefID          string              `json:"refId"`
	Title          string              `json:"title"`
	Body           string              `json:"body"`
	Href           string              `json:"href"`
	OccurrenceDate wiretime.Date       `json:"occurrenceDate"`
	SentAt         wiretime.Timestamp  `json:"sentAt"`
	DismissedAt    *wiretime.Timestamp `json:"dismissedAt,omitempty"`
}

// PushConfig tells the browser whether Web Push can be enabled and with which
// VAPID public key.
type PushConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey"`
}

// SubscribeRequest is the POST /push/subscriptions body.
type SubscribeRequest struct {
	Endpoint string           `json:"endpoint"`
	Keys     SubscriptionKeys `json:"keys"`
}

// SubscriptionKeys are the browser's encryption keys for one subscription.
type SubscriptionKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// UnsubscribeRequest is the DELETE /push/subscriptions body.
type UnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// SettingsRow is the persisted shape of the reminder settings.
type SettingsRow struct {
	UserID           string         `gorm:"column:user_id;primaryKey"`
	Enabled          bool           `gorm:"column:enabled"`
	TaskTime         wiretime.Clock `gorm:"column:task_time"`
	EventLeadMinutes int32          `gorm:"column:event_lead_minutes"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
}

// TableName keeps the Gorm mapping explicit.
func (SettingsRow) TableName() string {
	return "reminder_settings"
}

// ToWire converts a stored row into the API shape.
func (r SettingsRow) ToWire() Settings {
	return Settings{
		Enabled:          r.Enabled,
		TaskTime:         r.TaskTime.String(),
		EventLeadMinutes: r.EventLeadMinutes,
	}
}

// SubscriptionRow is the persisted shape of one push subscription.
type SubscriptionRow struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Endpoint   string     `gorm:"column:endpoint"`
	P256dh     string     `gorm:"column:p256dh"`
	Auth       string     `gorm:"column:auth"`
	UserAgent  *string    `gorm:"column:user_agent"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
}

// TableName keeps the Gorm mapping explicit.
func (SubscriptionRow) TableName() string {
	return "push_subscriptions"
}

// ToPush converts a stored subscription into the library shape.
func (r SubscriptionRow) ToPush() webpush.Subscription {
	return webpush.Subscription{
		Endpoint: r.Endpoint,
		Keys: webpush.Keys{
			P256dh: r.P256dh,
			Auth:   r.Auth,
		},
	}
}

// DeliveryRow is the persisted shape of one delivered reminder.
type DeliveryRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	UserID         string     `gorm:"column:user_id"`
	Kind           string     `gorm:"column:kind"`
	RefID          string     `gorm:"column:ref_id"`
	OccurrenceDate time.Time  `gorm:"column:occurrence_date"`
	Title          string     `gorm:"column:title"`
	Body           string     `gorm:"column:body"`
	Href           string     `gorm:"column:href"`
	SentAt         time.Time  `gorm:"column:sent_at"`
	DismissedAt    *time.Time `gorm:"column:dismissed_at"`
}

// TableName keeps the Gorm mapping explicit.
func (DeliveryRow) TableName() string {
	return "reminder_deliveries"
}

// ToWire converts a stored delivery into the API shape.
func (r DeliveryRow) ToWire() Reminder {
	var dismissedAt *wiretime.Timestamp
	if r.DismissedAt != nil {
		value := wiretime.Timestamp(*r.DismissedAt)
		dismissedAt = &value
	}

	return Reminder{
		ID:             r.ID,
		Kind:           r.Kind,
		RefID:          r.RefID,
		Title:          r.Title,
		Body:           r.Body,
		Href:           r.Href,
		OccurrenceDate: wiretime.Date(r.OccurrenceDate),
		SentAt:         wiretime.Timestamp(r.SentAt),
		DismissedAt:    dismissedAt,
	}
}
