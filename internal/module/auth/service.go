package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/alexedwards/argon2id"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"morrow-api/internal/config"
	"morrow-api/internal/database"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/reminder"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/tokens"
	"morrow-api/internal/platform/wiretime"
)

// Service owns registration, login, logout, and bearer resolution.
type Service struct {
	db            *gorm.DB
	cfg           *config.Config
	log           *slog.Logger
	accounts      *Repository
	profiles      *profile.Repository
	settingsRepo  *settings.Repository
	remindersRepo *reminder.Repository
}

// NewService wires the auth service.
func NewService(
	db *gorm.DB,
	cfg *config.Config,
	log *slog.Logger,
	accounts *Repository,
	profiles *profile.Repository,
	settingsRepo *settings.Repository,
	remindersRepo *reminder.Repository,
) *Service {
	return &Service{
		db:            db,
		cfg:           cfg,
		log:           log,
		accounts:      accounts,
		profiles:      profiles,
		settingsRepo:  settingsRepo,
		remindersRepo: remindersRepo,
	}
}

// HashPassword produces the Argon2id PHC string stored in `users`.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// VerifyPassword accepts any Argon2id PHC string, including hashes written by
// the previous Rust implementation.
func VerifyPassword(password, hash string) bool {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && match
}

// Register creates the account, its profile, default settings, and the first
// session in one transaction.
func (s *Service) Register(ctx context.Context, request RegisterRequest) (string, profile.Profile, error) {
	name, email, err := validateRegistration(request)
	if err != nil {
		return "", profile.Profile{}, err
	}

	passwordHash, err := HashPassword(request.Password)
	if err != nil {
		return "", profile.Profile{}, httpx.Internal(err, s.log)
	}

	now := wiretime.Now()
	userID := ids.New("u")
	token := tokens.Session()
	expiresAt := now.Time().AddDate(0, 0, s.cfg.TokenTTLDays)

	current := profile.Profile{
		ID:        profile.WireID,
		Name:      name,
		Email:     email,
		JoinedAt:  now,
		UpdatedAt: now,
	}

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		inUse, err := s.accounts.EmailInUse(ctx, tx, email, "")
		if err != nil {
			return err
		}
		if inUse {
			return httpx.Conflict("Email already in use")
		}

		if err := s.accounts.InsertUser(ctx, tx, UserRow{
			ID:           userID,
			Email:        email,
			PasswordHash: passwordHash,
			CreatedAt:    now.Time(),
		}); err != nil {
			return err
		}

		if err := s.profiles.Insert(ctx, tx, userID, current); err != nil {
			return err
		}
		if err := s.settingsRepo.EnsureDefaults(ctx, tx, userID); err != nil {
			return err
		}
		if err := s.remindersRepo.EnsureDefaults(ctx, tx, userID); err != nil {
			return err
		}

		return s.accounts.InsertSession(
			ctx, tx, ids.New("s"), userID, tokens.TokenHash(token), now.Time(), expiresAt,
		)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", profile.Profile{}, httpx.Conflict("Email already in use")
		}

		return "", profile.Profile{}, httpx.Map(err, s.log)
	}

	return token, current, nil
}

// Login validates the credentials, prunes expired sessions, and issues a token.
func (s *Service) Login(ctx context.Context, request LoginRequest) (string, profile.Profile, error) {
	email := strings.ToLower(strings.TrimSpace(request.Email))

	credentials, err := s.accounts.FindCredentials(ctx, email)
	if err != nil {
		return "", profile.Profile{}, httpx.Internal(err, s.log)
	}
	if credentials == nil || !VerifyPassword(request.Password, credentials.PasswordHash) {
		return "", profile.Profile{}, invalidCredentials()
	}

	if err := s.accounts.DeleteExpiredSessions(ctx, credentials.ID); err != nil {
		return "", profile.Profile{}, httpx.Internal(err, s.log)
	}

	token := tokens.Session()
	issuedAt := wiretime.Now().Time()
	expiresAt := issuedAt.AddDate(0, 0, s.cfg.TokenTTLDays)

	err = database.Tx(ctx, s.db, func(tx *gorm.DB) error {
		return s.accounts.InsertSession(ctx, tx, ids.New("s"), credentials.ID, tokens.TokenHash(token), issuedAt, expiresAt)
	})
	if err != nil {
		return "", profile.Profile{}, httpx.Internal(err, s.log)
	}

	current, err := s.profiles.Get(ctx, credentials.ID)
	if err != nil {
		return "", profile.Profile{}, httpx.Internal(err, s.log)
	}
	if current == nil {
		return "", profile.Profile{}, httpx.Internal(errors.New("Profile missing for user"), s.log)
	}

	return token, *current, nil
}

// Logout deletes the session; personal access tokens have none, so it is a no-op.
func (s *Service) Logout(ctx context.Context, sessionID *string) error {
	if sessionID == nil {
		return nil
	}

	if err := s.accounts.DeleteSession(ctx, *sessionID); err != nil {
		return httpx.Internal(err, s.log)
	}

	return nil
}

// SessionProfile returns the profile for GET /auth/session.
func (s *Service) SessionProfile(ctx context.Context, userID string) (*profile.Profile, error) {
	current, err := s.profiles.Get(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}
	if current == nil {
		return nil, httpx.NotFound("Profile not found")
	}

	return current, nil
}

// Authenticate resolves either credential kind. Personal access tokens return a
// nil session id and refresh `last_used_at` best-effort.
func (s *Service) Authenticate(ctx context.Context, raw string) (string, *string, error) {
	if tokens.IsPAT(raw) {
		hash := tokens.TokenHash(raw)

		userID, err := s.accounts.FindAPITokenUser(ctx, hash)
		if err != nil {
			return "", nil, httpx.Internal(err, s.log)
		}
		if userID == nil {
			return "", nil, httpx.Unauthorized()
		}

		if err := s.accounts.TouchAPIToken(ctx, hash); err != nil {
			s.log.Debug("touch api token failed", "error", err)
		}

		return *userID, nil, nil
	}

	session, err := s.accounts.FindActiveSession(ctx, tokens.TokenHash(raw))
	if err != nil {
		return "", nil, httpx.Internal(err, s.log)
	}
	if session == nil {
		return "", nil, httpx.Unauthorized()
	}

	return session.UserID, &session.SessionID, nil
}
