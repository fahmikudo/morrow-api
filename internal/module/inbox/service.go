package inbox

import (
	"context"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/wiretime"
)

// Service applies the inbox rules inside one transaction per write.
type Service struct {
	db   *gorm.DB
	log  *slog.Logger
	repo *Repository
}

// NewService wires the inbox service.
func NewService(db *gorm.DB, log *slog.Logger, repo *Repository) *Service {
	return &Service{db: db, log: log, repo: repo}
}

// List returns the active items.
func (s *Service) List(ctx context.Context, userID string) ([]Item, error) {
	items, err := s.repo.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return items, nil
}

// ListArchived returns the archived items.
func (s *Service) ListArchived(ctx context.Context, userID string) ([]Item, error) {
	items, err := s.repo.ListArchived(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return items, nil
}

// Get returns one item or a 404.
func (s *Service) Get(ctx context.Context, userID, id string) (*Item, error) {
	current, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Inbox item not found")
	}

	return current, nil
}

// Create trims the title and stores the item as active.
func (s *Service) Create(ctx context.Context, userID string, input New) (*Item, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, httpx.BadRequest("Title is required")
	}

	created := Item{
		ID:        ids.New("i"),
		Title:     title,
		Content:   input.Content,
		Type:      input.Type,
		CreatedAt: wiretime.Now(),
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

// Update applies a PATCH body; a blank title is rejected, otherwise stored as sent.
func (s *Service) Update(ctx context.Context, userID, id string, patch Update) (*Item, error) {
	var updated Item

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Inbox item not found")
		}

		if patch.Title != nil {
			if strings.TrimSpace(*patch.Title) == "" {
				return httpx.BadRequest("Title is required")
			}
			row.Title = *patch.Title
		}
		if patch.Content != nil {
			row.Content = patch.Content
		}
		if patch.Type != nil {
			row.Type = *patch.Type
		}

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

// Archive stamps `archived_at` once and keeps the first instant on repeats.
func (s *Service) Archive(ctx context.Context, userID, id string) (*Item, error) {
	return s.flipArchive(ctx, userID, id, true)
}

// Restore clears `archived_at`.
func (s *Service) Restore(ctx context.Context, userID, id string) (*Item, error) {
	return s.flipArchive(ctx, userID, id, false)
}

func (s *Service) flipArchive(ctx context.Context, userID, id string, archived bool) (*Item, error) {
	var updated Item

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		row, err := s.repo.GetForUpdate(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if row == nil {
			return httpx.NotFound("Inbox item not found")
		}

		if archived {
			if row.ArchivedAt == nil {
				now := wiretime.Now().Time()
				row.ArchivedAt = &now
			}
		} else {
			row.ArchivedAt = nil
		}

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

// Remove deletes the item.
func (s *Service) Remove(ctx context.Context, userID, id string) error {
	return httpx.Map(s.repo.Delete(ctx, userID, id), s.log)
}
