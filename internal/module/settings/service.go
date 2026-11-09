package settings

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/config"
	"morrow-api/internal/database"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/platform/features"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/secretbox"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the settings state machine inside one transaction per write.
type Service struct {
	db       *gorm.DB
	cfg      *config.Config
	log      *slog.Logger
	repo     *Repository
	profiles *profile.Repository
	client   *http.Client

	catalogueMu    sync.Mutex
	catalogueCache map[string]catalogueEntry
}

type catalogueEntry struct {
	models []AIModel
	at     time.Time
}

// NewService wires the settings service.
func NewService(
	db *gorm.DB,
	cfg *config.Config,
	log *slog.Logger,
	repo *Repository,
	profiles *profile.Repository,
	client *http.Client,
) *Service {
	return &Service{db: db, cfg: cfg, log: log, repo: repo, profiles: profiles, client: client}
}

// Get returns the integrations and the assistant settings.
func (s *Service) Get(ctx context.Context, userID string) (*Response, error) {
	integrations, err := s.repo.Integrations(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	record, err := s.repo.AIRecord(ctx, nil, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &Response{
		Integrations:             integrations,
		AI:                       record.ToWire(),
		AIModels:                 s.catalogue(ctx, record.APIKeyValue()),
		AIEffort:                 record.ReasoningEffort,
		GoogleCalendarConfigured: s.googleCalendarConfigured(),
		Features: map[string]bool{
			"softwareseni-pepy": s.pepyEnabled(ctx, userID),
		},
	}, nil
}

func (s *Service) pepyEnabled(ctx context.Context, userID string) bool {
	if s.profiles == nil {
		return false
	}
	current, err := s.profiles.Get(ctx, userID)
	if err != nil || current == nil {
		return false
	}

	return features.PepyEnabled(s.cfg, current.Email)
}

func (s *Service) googleCalendarConfigured() bool {
	if s.cfg.GoogleClientID == "" || s.cfg.GoogleClientSecret == "" || s.cfg.GoogleRedirectURL == "" {
		return false
	}

	box, err := secretbox.New(s.cfg.AppEncryptionKey)
	return err == nil && box.Enabled()
}

// AI returns the wire settings plus the stored record other services need.
func (s *Service) AI(ctx context.Context, userID string) (AISettings, *AIRow, error) {
	record, err := s.repo.AIRecord(ctx, nil, userID)
	if err != nil {
		return AISettings{}, nil, httpx.Map(err, s.log)
	}

	return record.ToWire(), record, nil
}

// Integrations returns every integration for the user.
func (s *Service) Integrations(ctx context.Context, userID string) (map[string]IntegrationConnection, error) {
	integrations, err := s.repo.Integrations(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return integrations, nil
}

// SetIntegrationEnabled stores the toggle and always disconnects.
func (s *Service) SetIntegrationEnabled(ctx context.Context, userID, integrationID string, enabled bool) (*IntegrationConnection, error) {
	var updated IntegrationConnection

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		connection, err := s.repo.Integration(ctx, tx, userID, integrationID)
		if err != nil {
			return err
		}
		if connection == nil {
			return httpx.NotFound("Integration not found")
		}

		connection.Enabled = enabled
		connection.Connected = false

		if err := s.repo.SetIntegration(ctx, tx, userID, integrationID, *connection); err != nil {
			return err
		}

		updated = *connection
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

// Connect marks the integration connected and stamps `last_sync_at`.
func (s *Service) Connect(ctx context.Context, userID, integrationID string) (*IntegrationConnection, error) {
	return s.mutate(ctx, userID, integrationID, func(connection *IntegrationConnection) error {
		if !connection.Enabled {
			return httpx.BadRequest("Integration is disabled")
		}

		connection.Connected = true
		now := wiretime.Now()
		connection.LastSyncAt = &now

		return nil
	})
}

// Disconnect clears the connected flag and keeps `last_sync_at`.
func (s *Service) Disconnect(ctx context.Context, userID, integrationID string) (*IntegrationConnection, error) {
	return s.mutate(ctx, userID, integrationID, func(connection *IntegrationConnection) error {
		connection.Connected = false
		return nil
	})
}

// Sync refreshes `last_sync_at` for an enabled, connected integration.
func (s *Service) Sync(ctx context.Context, userID, integrationID string) (*IntegrationConnection, error) {
	return s.mutate(ctx, userID, integrationID, func(connection *IntegrationConnection) error {
		if !connection.Enabled || !connection.Connected {
			return httpx.BadRequest("Integration is not connected")
		}

		now := wiretime.Now()
		connection.LastSyncAt = &now

		return nil
	})
}

// UpdateAI applies the assistant patch; an unknown model rolls the whole
// transaction back.
func (s *Service) UpdateAI(ctx context.Context, userID string, patch UpdateAI) (*AISettings, error) {
	current, err := s.repo.AIRecord(ctx, nil, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	keyForModels := current.APIKeyValue()
	if patch.APIKey.Set && !patch.APIKey.Null {
		keyForModels = strings.TrimSpace(patch.APIKey.Value)
	}
	if patch.Model != nil && !s.modelKnown(ctx, keyForModels, *patch.Model) {
		return nil, httpx.BadRequest("Unknown model")
	}
	if patch.ReasoningEffort.Set && !patch.ReasoningEffort.Null &&
		!contains(EffortLevels, patch.ReasoningEffort.Value) {
		return nil, httpx.BadRequest("Unknown reasoning effort")
	}

	var updated AISettings

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		record, err := s.repo.AIRecord(ctx, tx, userID)
		if err != nil {
			return err
		}

		if patch.Enabled != nil {
			record.Enabled = *patch.Enabled
		}

		if patch.Model != nil {
			record.Model = *patch.Model
		}

		if patch.ReasoningEffort.Set {
			record.ReasoningEffort = nil
			if !patch.ReasoningEffort.Null {
				value := patch.ReasoningEffort.Value
				record.ReasoningEffort = &value
			}
		}

		if patch.APIKey.Set {
			record.APIKey = nil
			record.Status = "untested"
			record.LastTestedAt = nil

			if !patch.APIKey.Null {
				value := strings.TrimSpace(patch.APIKey.Value)
				if value == "" {
					return httpx.BadRequest("API key cannot be empty")
				}
				if len(value) > 512 {
					return httpx.BadRequest("Keep the API key under 512 characters")
				}
				record.APIKey = &value
			}
		}

		if err := s.repo.SetAIRecord(ctx, tx, *record); err != nil {
			return err
		}

		updated = record.ToWire()
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

// TestAI probes DeepSeek and persists the outcome.
func (s *Service) TestAI(ctx context.Context, userID string) (*TestResult, error) {
	var result TestResult

	current, err := s.repo.AIRecord(ctx, nil, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	ok, message := TestConnection(ctx, s.client, s.cfg, current.APIKeyValue())

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		record, err := s.repo.AIRecord(ctx, tx, userID)
		if err != nil {
			return err
		}

		status := "error"
		if ok {
			status = "ok"
		}
		testedAt := wiretime.Now()

		record.Status = status
		record.LastTestedAt = new(testedAt.Time())

		if err := s.repo.SetAIRecord(ctx, tx, *record); err != nil {
			return err
		}

		result = TestResult{OK: ok, Status: status, Message: message, TestedAt: testedAt}
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &result, nil
}

func (s *Service) mutate(
	ctx context.Context,
	userID, integrationID string,
	apply func(connection *IntegrationConnection) error,
) (*IntegrationConnection, error) {
	var updated IntegrationConnection

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		connection, err := s.repo.Integration(ctx, tx, userID, integrationID)
		if err != nil {
			return err
		}
		if connection == nil {
			return httpx.NotFound("Integration not found")
		}

		if err := apply(connection); err != nil {
			return err
		}

		if err := s.repo.SetIntegration(ctx, tx, userID, integrationID, *connection); err != nil {
			return err
		}

		updated = *connection
		return nil
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &updated, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
