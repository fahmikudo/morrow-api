package note

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/project"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/pgarray"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the note rules: titles are free-form, tags are kept as sent.
type Service struct {
	db          *gorm.DB
	log         *slog.Logger
	repo        *Repository
	projects    *project.Repository
	areas       *area.Repository
	attachments AttachmentCleaner
}

// AttachmentCleaner removes files attached to a deleted entity.
type AttachmentCleaner interface {
	DeleteForEntity(ctx context.Context, userID, entityType, entityID string)
}

// NewService wires the note service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	projects *project.Repository,
	areas *area.Repository,
	attachments AttachmentCleaner,
) *Service {
	return &Service{
		db: db, log: log, repo: repo, projects: projects, areas: areas, attachments: attachments,
	}
}

// List returns every note.
func (s *Service) List(ctx context.Context, userID string) ([]Note, error) {
	notes, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return notes, nil
}

// Get returns one note or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Note, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Note not found")
	}

	return current, nil
}

// Create stores the note with empty content and tags by default.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Note, error) {
	var created Note

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		if input.ProjectID != nil {
			if err := s.requireProject(ctx, tx, userID, *input.ProjectID); err != nil {
				return err
			}
		}
		if input.AreaID != nil {
			if err := s.requireArea(ctx, tx, userID, *input.AreaID); err != nil {
				return err
			}
		}

		now := wiretime.Now()

		created = Note{
			ID:        ids.New("n"),
			Title:     input.Title,
			Content:   "",
			Tags:      []string{},
			ProjectID: input.ProjectID,
			AreaID:    input.AreaID,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if input.Content != nil {
			created.Content = *input.Content
		}
		if input.Tags != nil {
			created.Tags = input.Tags
		}

		row := Row{UserID: userID}
		row.Load(created)

		return s.repo.Insert(ctx, tx, row)
	})
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &created, nil
}

// Update applies a PATCH body.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Note, error) {
	var updated Note

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Note not found")
		}

		if patch.Title != nil {
			row.Title = *patch.Title
		}
		if patch.Content != nil {
			row.Content = *patch.Content
		}
		if patch.Tags != nil {
			row.Tags = pgarray.Texts(patch.Tags)
		}
		if patch.ProjectID.Set {
			row.ProjectID = nil
			if !patch.ProjectID.Null {
				value := patch.ProjectID.Value
				if err := s.requireProject(ctx, tx, userID, value); err != nil {
					return err
				}
				row.ProjectID = &value
			}
		}
		if patch.AreaID.Set {
			row.AreaID = nil
			if !patch.AreaID.Null {
				value := patch.AreaID.Value
				if err := s.requireArea(ctx, tx, userID, value); err != nil {
					return err
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

// Remove deletes the note.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return httpx.Map(err, s.log)
	}
	if s.attachments != nil {
		s.attachments.DeleteForEntity(ctx, userID, "note", id)
	}
	return nil
}

func (s *Service) requireProject(ctx context.Context, tx *gorm.DB, userID, id string) error {
	exists, err := s.projects.Exists(ctx, tx, userID, id)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.BadRequest("Project not found")
	}

	return nil
}

func (s *Service) requireArea(ctx context.Context, tx *gorm.DB, userID, id string) error {
	exists, err := s.areas.Exists(ctx, tx, userID, id)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.BadRequest("Area not found")
	}

	return nil
}
