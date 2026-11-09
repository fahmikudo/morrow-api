package area

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the area rules. Areas have no required fields beyond the name.
type Service struct {
	db   *gorm.DB
	log  *slog.Logger
	repo *Repository
}

// NewService wires the area service.
func NewService(db *gorm.DB, log *slog.Logger, repo *Repository) *Service {
	return &Service{db: db, log: log, repo: repo}
}

// List returns every area.
func (s *Service) List(ctx context.Context, userID string) ([]Area, error) {
	areas, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return areas, nil
}

// Get returns one area or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Area, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Area not found")
	}

	return current, nil
}

// Create stores the area with the default tone and icon.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Area, error) {
	now := wiretime.Now()

	created := Area{
		ID:          ids.New("a"),
		Name:        input.Name,
		Description: input.Description,
		Tone:        "neutral",
		Icon:        "sparkles",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if input.Tone != nil {
		created.Tone = *input.Tone
	}
	if input.Icon != nil {
		created.Icon = *input.Icon
	}

	row := Row{UserID: userID}
	row.Load(created)

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		return s.repo.Insert(ctx, tx, row)
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &created, nil
}

// Update applies a PATCH body.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Area, error) {
	var updated Area

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Area not found")
		}

		if patch.Name != nil {
			row.Name = *patch.Name
		}
		if patch.Description != nil {
			row.Description = patch.Description
		}
		if patch.Tone != nil {
			row.Tone = *patch.Tone
		}
		if patch.Icon != nil {
			row.Icon = *patch.Icon
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

// Remove deletes the area; references are cleared by the foreign keys.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	return httpx.Map(s.repo.Delete(ctx, userID, id), s.log)
}
