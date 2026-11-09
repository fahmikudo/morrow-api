package reminder

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/timezone"
	"morrow-api/internal/platform/wiretime"
)

// Scheduler claims due reminders and pushes them to subscribed devices. One
// process owns the loop; the delivery unique key makes a repeated tick
// idempotent.
type Scheduler struct {
	db               *gorm.DB
	log              *slog.Logger
	repo             *Repository
	tasks            *task.Repository
	events           *event.Repository
	habits           *habit.Repository
	profiles         *profile.Repository
	sender           Sender
	tick             time.Duration
	fallbackTimezone string
}

// NewScheduler wires the background reminder loop.
func NewScheduler(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	tasks *task.Repository,
	events *event.Repository,
	habits *habit.Repository,
	profiles *profile.Repository,
	sender Sender,
	tick time.Duration,
	fallbackTimezone string,
) *Scheduler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if tick <= 0 {
		tick = time.Minute
	}

	return &Scheduler{
		db:               db,
		log:              log,
		repo:             repo,
		tasks:            tasks,
		events:           events,
		habits:           habits,
		profiles:         profiles,
		sender:           sender,
		tick:             tick,
		fallbackTimezone: fallbackTimezone,
	}
}

// Run executes one pass immediately, then once per tick until the context ends.
func (s *Scheduler) Run(ctx context.Context) {
	s.log.Info("reminder scheduler started", "tick_seconds", int(s.tick.Seconds()))

	if err := s.RunOnce(ctx); err != nil {
		s.log.Warn("reminder tick failed", "error", err)
	}

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunOnce(ctx); err != nil {
				s.log.Warn("reminder tick failed", "error", err)
			}
		}
	}
}

// RunOnce processes every user whose reminders are enabled. A failure for one
// user is logged and never stops the others.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	userIDs, err := s.repo.EnabledUserIDs(ctx)
	if err != nil {
		return err
	}

	now := wiretime.Now().Time()
	for _, userID := range userIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := s.runUser(ctx, userID, now); err != nil {
			s.log.Warn("reminder run failed", "user_id", userID, "error", err)
		}
	}

	return nil
}

func (s *Scheduler) runUser(ctx context.Context, userID string, now time.Time) error {
	settingsRow, err := s.repo.Settings(ctx, nil, userID)
	if err != nil {
		return err
	}
	if !settingsRow.Enabled {
		return nil
	}

	loc := s.location(ctx, userID)

	tasks, err := s.tasks.List(ctx, userID)
	if err != nil {
		return err
	}

	events, err := s.events.List(ctx, userID)
	if err != nil {
		return err
	}

	habits, err := s.habits.ListActive(ctx, userID)
	if err != nil {
		return err
	}

	checkIns, err := s.habits.ListCheckIns(ctx, userID)
	if err != nil {
		return err
	}

	today := localToday(now, loc)
	candidates := computeDue(now, loc, settingsRow.ToWire(), ruleData{
		Tasks:    tasks,
		Events:   events,
		Habits:   habits,
		CheckIns: checkIns,
	})
	if len(candidates) == 0 {
		return nil
	}

	occurrence, err := wiretime.ParseDate(today)
	if err != nil {
		return err
	}

	claimed := make([]DeliveryRow, 0, len(candidates))

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		for _, item := range candidates {
			row := DeliveryRow{
				ID:             ids.New("rem"),
				UserID:         userID,
				Kind:           item.Kind,
				RefID:          item.RefID,
				OccurrenceDate: occurrence.Time(),
				Title:          item.Title,
				Body:           item.Body,
				Href:           item.Href,
				SentAt:         now,
			}

			won, err := s.repo.ClaimDelivery(ctx, tx, row)
			if err != nil {
				return err
			}
			if won {
				claimed = append(claimed, row)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	if len(claimed) == 0 || s.sender == nil || !s.sender.Enabled() {
		return nil
	}

	subscriptions, err := s.repo.Subscriptions(ctx, userID)
	if err != nil {
		return err
	}

	dropped := make(map[string]struct{}, len(subscriptions))
	for _, row := range claimed {
		payload := Payload{
			Title: row.Title,
			Body:  row.Body,
			URL:   row.Href,
			Tag:   row.Kind + ":" + row.RefID + ":" + today,
		}

		for _, sub := range subscriptions {
			if _, gone := dropped[sub.ID]; gone {
				continue
			}

			err := s.sender.Send(ctx, Subscription{
				Endpoint: sub.Endpoint,
				P256dh:   sub.P256dh,
				Auth:     sub.Auth,
			}, payload)
			if err == nil {
				continue
			}

			if errors.Is(err, ErrSubscriptionGone) {
				dropped[sub.ID] = struct{}{}
				if deleteErr := s.repo.DeleteSubscription(ctx, sub.ID); deleteErr != nil {
					s.log.Warn("drop gone push subscription", "subscription_id", sub.ID, "error", deleteErr)
				}
				continue
			}

			s.log.Warn("push reminder failed", "user_id", userID, "subscription_id", sub.ID, "error", err)
		}
	}

	return nil
}

// location prefers the user's profile timezone, then APP_TIMEZONE, then UTC.
func (s *Scheduler) location(ctx context.Context, userID string) *time.Location {
	if s.profiles == nil {
		return timezone.Resolve(nil, s.fallbackTimezone)
	}

	var profileTimezone *string
	if current, err := s.profiles.Get(ctx, userID); err == nil && current != nil {
		profileTimezone = current.Timezone
	}

	return timezone.Resolve(profileTimezone, s.fallbackTimezone)
}
