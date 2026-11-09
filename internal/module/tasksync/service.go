package tasksync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// ErrUnavailable means the provider is feature-gated off for this account.
var ErrUnavailable = errors.New("task sync is not available")

// Service owns provider links, reconciliation, and the background worker.
type Service struct {
	db        *gorm.DB
	log       *slog.Logger
	repo      *Repository
	tasks     *task.Repository
	providers map[string]Provider
	tick      time.Duration
}

// NewService wires the generic task sync service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	tasks *task.Repository,
	tick time.Duration,
	providers ...Provider,
) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	registry := make(map[string]Provider, len(providers))
	for _, provider := range providers {
		registry[provider.ID()] = provider
	}

	return &Service{
		db: db, log: log, repo: repo, tasks: tasks, providers: registry, tick: tick,
	}
}

func (s *Service) provider() Provider {
	return s.providers[PepyIntegrationID]
}

// Available reports whether Pepy is enabled for the user.
func (s *Service) Available(ctx context.Context, userID string) bool {
	provider := s.provider()
	return provider != nil && provider.EnabledFor(ctx, userID)
}

// EnabledTaskIDs returns tasks opted into Pepy.
func (s *Service) EnabledTaskIDs(ctx context.Context, userID string) (map[string]bool, error) {
	if !s.Available(ctx, userID) {
		return map[string]bool{}, nil
	}

	return s.repo.EnabledTaskIDs(ctx, userID, PepyIntegrationID)
}

// SetEnabled creates or toggles the Pepy link for a task.
func (s *Service) SetEnabled(ctx context.Context, userID, taskID string, enabled bool) error {
	if !s.Available(ctx, userID) {
		return ErrUnavailable
	}

	link, err := s.repo.Link(ctx, userID, taskID, PepyIntegrationID)
	if err != nil {
		return err
	}
	if link == nil {
		if !enabled {
			return nil
		}
		ref, err := newUUID()
		if err != nil {
			return err
		}
		return s.repo.InsertLink(ctx, LinkRow{
			ID:            ids.New("tlink"),
			UserID:        userID,
			TaskID:        &taskID,
			IntegrationID: PepyIntegrationID,
			ExternalRef:   &ref,
			Enabled:       true,
		})
	}

	link.Enabled = enabled
	return s.repo.SaveLink(ctx, *link)
}

// Push mirrors one opted-in task to Pepy. A failure is recorded on the link and
// never fails the local write.
func (s *Service) Push(ctx context.Context, userID string, current task.Task) error {
	provider := s.provider()
	if provider == nil || !provider.EnabledFor(ctx, userID) || !provider.Connected(ctx, userID) {
		return nil
	}

	link, err := s.repo.Link(ctx, userID, current.ID, PepyIntegrationID)
	if err != nil || link == nil || !link.Enabled {
		return err
	}

	return s.pushLink(ctx, userID, current, link, provider)
}

// Delete mirrors local deletes. Failed remote deletes stay as tombstones.
func (s *Service) Delete(ctx context.Context, userID, taskID string) error {
	links, err := s.repo.LinksForTask(ctx, userID, taskID)
	if err != nil {
		return err
	}

	for index := range links {
		link := &links[index]
		provider := s.providers[link.IntegrationID]
		if provider == nil || link.ExternalID == nil {
			if err := s.repo.DeleteLink(ctx, link.ID); err != nil {
				return err
			}
			continue
		}
		if !provider.EnabledFor(ctx, userID) || !provider.Connected(ctx, userID) {
			continue
		}
		if err := provider.Delete(ctx, userID, *link); err != nil {
			message := err.Error()
			link.LastError = &message
			_ = s.repo.SaveLink(ctx, *link)
			return err
		}
		if err := s.repo.DeleteLink(ctx, link.ID); err != nil {
			return err
		}
	}

	return nil
}

// Connect logs into Pepy and returns the account identity.
func (s *Service) Connect(ctx context.Context, userID string) (Account, error) {
	provider := s.provider()
	if provider == nil || !provider.EnabledFor(ctx, userID) {
		return Account{}, ErrUnavailable
	}

	return provider.Connect(ctx, userID)
}

// Disconnect drops the Pepy credential; task links stay for a reconnect.
func (s *Service) Disconnect(ctx context.Context, userID string) error {
	return s.repo.DeleteCredential(ctx, userID, PepyIntegrationID)
}

// Reconcile pushes linked tasks that changed or were never created. force
// re-pushes every enabled link, used by the manual Sync action after a
// timezone or account-level change.
func (s *Service) Reconcile(ctx context.Context, userID string, force bool) error {
	provider := s.provider()
	if provider == nil || !provider.EnabledFor(ctx, userID) || !provider.Connected(ctx, userID) {
		return nil
	}
	if err := provider.EnsureToken(ctx, userID); err != nil {
		return err
	}

	tasks, err := s.tasks.List(ctx, userID)
	if err != nil {
		return err
	}
	byID := make(map[string]task.Task, len(tasks))
	for _, current := range tasks {
		byID[current.ID] = current
	}

	links, err := s.repo.Links(ctx, userID, PepyIntegrationID)
	if err != nil {
		return err
	}

	for index := range links {
		link := &links[index]
		var current task.Task
		exists := false
		if link.TaskID != nil {
			current, exists = byID[*link.TaskID]
		}
		if link.TaskID == nil || !exists {
			if link.ExternalID == nil {
				if err := s.repo.DeleteLink(ctx, link.ID); err != nil {
					return err
				}
				continue
			}
			if err := provider.Delete(ctx, userID, *link); err != nil {
				message := err.Error()
				link.LastError = &message
				_ = s.repo.SaveLink(ctx, *link)
				return err
			}
			if err := s.repo.DeleteLink(ctx, link.ID); err != nil {
				return err
			}
			continue
		}

		if !link.Enabled {
			continue
		}
		if force || link.ExternalID == nil || link.LastSyncedAt == nil ||
			current.UpdatedAt.Time().After(link.LastSyncedAt.Add(time.Second)) {
			if err := s.pushLink(ctx, userID, current, link, provider); err != nil {
				return err
			}
		}
	}

	return nil
}

// Run reconciles connected users immediately and then on the configured tick.
func (s *Service) Run(ctx context.Context) {
	tick := s.tick
	if tick <= 0 {
		tick = 5 * time.Minute
	}

	s.log.Info("task sync started", "tick_seconds", int(tick.Seconds()))
	s.runAll(ctx)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAll(ctx)
		}
	}
}

func (s *Service) runAll(ctx context.Context) {
	provider := s.provider()
	if provider == nil {
		return
	}

	userIDs, err := s.repo.CredentialUserIDs(ctx, PepyIntegrationID)
	if err != nil {
		s.log.Warn("task sync user list failed", "error", err)
		return
	}

	for _, userID := range userIDs {
		if ctx.Err() != nil {
			return
		}
		if !provider.EnabledFor(ctx, userID) {
			continue
		}
		if err := provider.EnsureToken(ctx, userID); err != nil {
			s.log.Warn("task sync token failed", "user_id", userID, "error", err)
			continue
		}
		if err := s.Reconcile(ctx, userID, false); err != nil {
			s.log.Warn("task sync failed", "user_id", userID, "error", err)
		}
	}
}

func (s *Service) pushLink(
	ctx context.Context,
	userID string,
	current task.Task,
	link *LinkRow,
	provider Provider,
) error {
	var err error
	if link.ExternalID == nil {
		ref := ""
		if link.ExternalRef != nil {
			ref = *link.ExternalRef
		}
		if ref == "" {
			ref, err = newUUID()
			if err != nil {
				return err
			}
			link.ExternalRef = &ref
		}

		externalID, createErr := provider.Create(ctx, userID, current, ref)
		err = createErr
		if err == nil {
			link.ExternalID = &externalID
		}
	} else {
		err = provider.Update(ctx, userID, current, *link)
	}

	now := wiretime.Now().Time()
	if err != nil {
		message := err.Error()
		link.LastError = &message
		_ = s.repo.SaveLink(ctx, *link)
		return err
	}

	link.LastError = nil
	link.LastSyncedAt = &now
	return s.repo.SaveLink(ctx, *link)
}

func newUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16],
	), nil
}
