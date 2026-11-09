package reminder

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/config"
	"morrow-api/internal/database"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/timezone"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the reminder settings and subscription rules.
type Service struct {
	db       *gorm.DB
	cfg      *config.Config
	log      *slog.Logger
	repo     *Repository
	profiles *profile.Repository
	sender   Sender
}

// NewService wires the reminder service.
func NewService(
	db *gorm.DB,
	cfg *config.Config,
	log *slog.Logger,
	repo *Repository,
	profiles *profile.Repository,
	sender Sender,
) *Service {
	return &Service{
		db:       db,
		cfg:      cfg,
		log:      log,
		repo:     repo,
		profiles: profiles,
		sender:   sender,
	}
}

// Settings returns the reminder settings, creating the default row if needed.
func (s *Service) Settings(ctx context.Context, userID string) (*Settings, error) {
	row, err := s.repo.Settings(ctx, nil, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	settings := row.ToWire()
	return &settings, nil
}

// UpdateSettings validates the patch before writing it in one transaction.
func (s *Service) UpdateSettings(ctx context.Context, userID string, patch SettingsUpdate) (*Settings, error) {
	if patch.TaskTime != nil {
		if err := wiretime.ValidateTimeOfDay(*patch.TaskTime); err != nil {
			return nil, httpx.BadRequest("Use an HH:mm time")
		}
	}

	if patch.EventLeadMinutes != nil &&
		(*patch.EventLeadMinutes < 0 || *patch.EventLeadMinutes > 1440) {
		return nil, httpx.BadRequest("Lead time must be between 0 and 1440")
	}

	var updated Settings

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.Settings(ctx, tx, userID)
		if err != nil {
			return err
		}

		if patch.Enabled != nil {
			row.Enabled = *patch.Enabled
		}

		if patch.TaskTime != nil {
			row.TaskTime = wiretime.Clock(*patch.TaskTime)
		}

		if patch.EventLeadMinutes != nil {
			row.EventLeadMinutes = *patch.EventLeadMinutes
		}

		row.UpdatedAt = wiretime.Now().Time()
		if err := s.repo.SaveSettings(ctx, tx, *row); err != nil {
			return err
		}

		updated = row.ToWire()
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

// Today returns the user's undismissed reminders for their local date.
func (s *Service) Today(ctx context.Context, userID string) ([]Reminder, error) {
	loc := s.location(ctx, userID)
	today := localToday(wiretime.Now().Time(), loc)

	rows, err := s.repo.Today(ctx, userID, today)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	reminders := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		reminders = append(reminders, row.ToWire())
	}

	return reminders, nil
}

// Dismiss hides one reminder for the user.
func (s *Service) Dismiss(ctx context.Context, userID, id string) error {
	changed, err := s.repo.Dismiss(ctx, userID, id, wiretime.Now().Time())
	if err != nil {
		return httpx.Map(err, s.log)
	}
	if !changed {
		return httpx.NotFound("Reminder not found")
	}

	return nil
}

// PushConfig reports whether server push is available and with which key.
func (s *Service) PushConfig() *PushConfig {
	enabled := s.sender != nil && s.sender.Enabled()
	config := PushConfig{Enabled: enabled}
	if enabled {
		config.PublicKey = s.cfg.VAPIDPublicKey
	}

	return &config
}

// Subscribe stores a device subscription; the same endpoint is rebound to the
// user that last registered it.
func (s *Service) Subscribe(
	ctx context.Context,
	userID string,
	request SubscribeRequest,
	userAgent *string,
) error {
	endpoint := strings.TrimSpace(request.Endpoint)
	if endpoint == "" || !strings.HasPrefix(endpoint, "https://") {
		return httpx.BadRequest("Endpoint is required")
	}

	if request.Keys.P256dh == "" || request.Keys.Auth == "" {
		return httpx.BadRequest("Subscription keys are required")
	}

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if err := s.repo.EnsureDefaults(ctx, tx, userID); err != nil {
			return err
		}

		return s.repo.UpsertSubscription(
			ctx, userID, endpoint, request.Keys.P256dh, request.Keys.Auth,
			userAgent, wiretime.Now().Time(),
		)
	})
	if err != nil {
		return httpx.Map(err, s.log)
	}

	return nil
}

// Unsubscribe removes the user's subscription for one endpoint; a missing row
// is not an error.
func (s *Service) Unsubscribe(ctx context.Context, userID, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return httpx.BadRequest("Endpoint is required")
	}

	if err := s.repo.DeleteSubscriptionByEndpoint(ctx, userID, endpoint); err != nil {
		return httpx.Map(err, s.log)
	}

	return nil
}

// location prefers the user's profile timezone, then APP_TIMEZONE, then UTC.
func (s *Service) location(ctx context.Context, userID string) *time.Location {
	var profileTimezone *string
	if current, err := s.profiles.Get(ctx, userID); err == nil && current != nil {
		profileTimezone = current.Timezone
	}

	return timezone.Resolve(profileTimezone, s.cfg.AppTimezone)
}
