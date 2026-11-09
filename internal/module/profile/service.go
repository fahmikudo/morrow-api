package profile

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

// AccountStore is the slice of the account repository the profile needs: the
// users table is the source of truth for the login email.
type AccountStore interface {
	EmailInUse(ctx context.Context, tx *gorm.DB, email, excludeUserID string) (bool, error)
	UpdateUserEmail(ctx context.Context, tx *gorm.DB, userID, email string) error
}

// Service applies the profile rules inside one transaction per write.
type Service struct {
	db       *gorm.DB
	log      *slog.Logger
	repo     *Repository
	accounts AccountStore
}

// NewService wires the profile service.
func NewService(db *gorm.DB, log *slog.Logger, repo *Repository, accounts AccountStore) *Service {
	return &Service{db: db, log: log, repo: repo, accounts: accounts}
}

// Get returns the profile or a 404.
func (s *Service) Get(ctx context.Context, userID string) (*Profile, error) {
	current, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Profile not found")
	}

	return current, nil
}

// Update applies a PATCH body; it validates the email against `users` and
// stores the name/email exactly as provided.
func (s *Service) Update(ctx context.Context, userID string, patch Update) (*Profile, error) {
	var updated Profile

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		current, err := s.repo.GetForUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		if current == nil {
			return httpx.NotFound("Profile not found")
		}

		if patch.Name != nil {
			if strings.TrimSpace(*patch.Name) == "" {
				return httpx.BadRequest("Name is required")
			}
			current.Name = *patch.Name
		}

		if patch.Email != nil {
			email := *patch.Email
			inUse, err := s.accounts.EmailInUse(ctx, tx, strings.TrimSpace(email), userID)
			if err != nil {
				return err
			}
			if inUse {
				return httpx.Conflict("Email already in use")
			}
			if err := s.accounts.UpdateUserEmail(ctx, tx, userID, strings.TrimSpace(email)); err != nil {
				return err
			}
			current.Email = email
		}

		if patch.Role.Set {
			current.Role = nil
			if !patch.Role.Null {
				value := patch.Role.Value
				current.Role = &value
			}
		}

		if patch.Timezone.Set {
			current.Timezone = nil
			if !patch.Timezone.Null {
				value := patch.Timezone.Value
				current.Timezone = &value
			}
		}

		current.UpdatedAt = wiretime.Now().Time()

		if err := s.repo.Save(ctx, tx, *current); err != nil {
			return err
		}

		updated = current.ToWire()
		return nil
	})
	if err != nil {
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, httpx.Internal(err, s.log)
	}

	return &updated, nil
}
