package apitoken

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"morrow-api/internal/database"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/tokens"
	"morrow-api/internal/platform/wiretime"
)

// NameMaxRunes caps the human label shown in Settings.
const NameMaxRunes = 80

// Service issues and revokes personal access tokens.
type Service struct {
	db     *gorm.DB
	log    *slog.Logger
	tokens *Repository
}

// NewService wires the token service.
func NewService(db *gorm.DB, log *slog.Logger, tokens *Repository) *Service {
	return &Service{db: db, log: log, tokens: tokens}
}

// List returns the user's tokens.
func (s *Service) List(ctx context.Context, userID string) ([]APIToken, error) {
	list, err := s.tokens.List(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}

	return list, nil
}

// Create allocates the token, stores its hash, and returns the plaintext once.
func (s *Service) Create(ctx context.Context, userID string, request CreateRequest) (*CreateResponse, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, httpx.BadRequest("Name is required")
	}
	if utf8.RuneCountInString(name) > NameMaxRunes {
		return nil, httpx.BadRequest("Name is too long")
	}

	raw := tokens.PAT()
	id := ids.New("tok")
	createdAt := wiretime.Now()

	err := database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		return s.tokens.Create(ctx, tx, Row{
			ID:        id,
			UserID:    userID,
			Name:      name,
			TokenHash: tokens.TokenHash(raw),
			CreatedAt: createdAt.Time(),
		})
	})
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}

	return &CreateResponse{
		Token: raw,
		APIToken: APIToken{
			ID:         id,
			Name:       name,
			CreatedAt:  createdAt,
			LastUsedAt: nil,
		},
	}, nil
}

// Revoke deletes one token, reporting 404 when it is not the user's.
func (s *Service) Revoke(ctx context.Context, userID, id string) error {
	deleted, err := s.tokens.Delete(ctx, userID, id)
	if err != nil {
		return httpx.Internal(err, s.log)
	}
	if !deleted {
		return httpx.NotFound("Token not found")
	}

	return nil
}
