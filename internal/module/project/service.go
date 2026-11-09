package project

import (
	"context"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/area"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/keygen"
	"morrow-api/internal/platform/wiretime"
)

// keyAttempts bounds the insert/retry loop on concurrent creates.
const keyAttempts = 10

// Service applies the project rules inside one transaction per write.
type Service struct {
	db          *gorm.DB
	log         *slog.Logger
	repo        *Repository
	areas       *area.Repository
	attachments AttachmentCleaner
}

// AttachmentCleaner removes files attached to a deleted entity.
type AttachmentCleaner interface {
	DeleteForEntity(ctx context.Context, userID, entityType, entityID string)
}

// NewService wires the project service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	areas *area.Repository,
	attachments AttachmentCleaner,
) *Service {
	return &Service{db: db, log: log, repo: repo, areas: areas, attachments: attachments}
}

// List returns every project.
func (s *Service) List(ctx context.Context, userID string) ([]Project, error) {
	projects, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return projects, nil
}

// Get returns one project or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Project, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Project not found")
	}

	return current, nil
}

// Create derives the key, retries on collisions, and stores the trimmed name.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Project, error) {
	var created Project

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if input.AreaID != nil {
			ok, err := s.areas.Exists(ctx, tx, userID, *input.AreaID)
			if err != nil {
				return err
			}
			if !ok {
				return httpx.BadRequest("Area not found")
			}
		}

		base := keygen.DeriveKeyPrefix(input.Name)
		now := wiretime.Now()
		status := "active"
		if input.Status != nil {
			status = *input.Status
		}

		for range keyAttempts {
			taken, err := s.repo.TakenKeys(ctx, tx, userID)
			if err != nil {
				return err
			}

			candidate := Project{
				ID:          ids.New("p"),
				Key:         keygen.UniqueKey(base, taken),
				Name:        strings.TrimSpace(input.Name),
				Description: input.Description,
				Status:      status,
				AreaID:      input.AreaID,
				CreatedAt:   now,
				UpdatedAt:   now,
			}

			row := Row{UserID: userID}
			row.Load(candidate)

			inserted, err := s.repo.InsertIfKeyFree(ctx, tx, row)
			if err != nil {
				return err
			}
			if inserted {
				created = candidate
				return nil
			}
		}

		return httpx.Conflict("Could not allocate a project key")
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &created, nil
}

// Update applies a PATCH body; the name is stored exactly as provided and the
// key is never regenerated.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Project, error) {
	var updated Project

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Project not found")
		}

		if patch.Name != nil {
			row.Name = *patch.Name
		}
		if patch.Description != nil {
			row.Description = patch.Description
		}
		if patch.Status != nil {
			row.Status = *patch.Status
		}
		if patch.AreaID.Set {
			row.AreaID = nil
			if !patch.AreaID.Null {
				value := patch.AreaID.Value
				ok, err := s.areas.Exists(ctx, tx, userID, value)
				if err != nil {
					return err
				}
				if !ok {
					return httpx.BadRequest("Area not found")
				}
				row.AreaID = &value
			}
		}

		row.UpdatedAt = wiretime.Now().Time()

		if err := s.repo.Save(ctx, tx, *row); err != nil {
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

// Remove deletes the project; the foreign keys clear every reference.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return httpx.Map(err, s.log)
	}
	if s.attachments != nil {
		s.attachments.DeleteForEntity(ctx, userID, "project", id)
	}
	return nil
}
