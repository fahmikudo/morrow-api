package auth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Repository holds every user and session statement plus the two personal
// access token lookups the bearer middleware needs.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the account repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// CountUsers returns the number of seeded accounts.
func (r *Repository) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UserRow{}).Count(&count).Error
	return count, err
}

// InsertUser creates an account row.
func (r *Repository) InsertUser(ctx context.Context, tx *gorm.DB, user UserRow) error {
	return tx.WithContext(ctx).Create(&user).Error
}

// FindCredentials looks the account up case-insensitively.
func (r *Repository) FindCredentials(ctx context.Context, email string) (*Credentials, error) {
	var row UserRow
	err := r.db.WithContext(ctx).Where("lower(email) = lower(?)", email).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &Credentials{ID: row.ID, PasswordHash: row.PasswordHash}, nil
}

// EmailInUse reports whether another account already owns the email.
// A nil transaction runs on the pool.
func (r *Repository) EmailInUse(ctx context.Context, tx *gorm.DB, email, excludeUserID string) (bool, error) {
	runner := r.db
	if tx != nil {
		runner = tx
	}

	var exists bool
	err := runner.WithContext(ctx).Raw(
		"select exists (select 1 from users where lower(email) = lower(?) and id <> ?)",
		email, excludeUserID,
	).Scan(&exists).Error

	return exists, err
}

// UpdateUserEmail keeps the users row in step with the profile.
func (r *Repository) UpdateUserEmail(ctx context.Context, tx *gorm.DB, userID, email string) error {
	return tx.WithContext(ctx).Model(&UserRow{}).Where("id = ?", userID).
		Update("email", email).Error
}

// InsertSession stores a hashed session token with its expiry.
func (r *Repository) InsertSession(
	ctx context.Context,
	tx *gorm.DB,
	id, userID, tokenHash string,
	createdAt, expiresAt time.Time,
) error {
	return tx.WithContext(ctx).Create(&SessionRow{
		ID:        id,
		UserID:    userID,
		TokenHash: tokenHash,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
	}).Error
}

// FindActiveSession resolves a hashed token that has not expired.
func (r *Repository) FindActiveSession(ctx context.Context, tokenHash string) (*ActiveSession, error) {
	var row SessionRow
	err := r.db.WithContext(ctx).
		Where("token_hash = ? and expires_at > now()", tokenHash).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &ActiveSession{SessionID: row.ID, UserID: row.UserID}, nil
}

// DeleteSession drops one session by id.
func (r *Repository) DeleteSession(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Where("id = ?", sessionID).Delete(&SessionRow{}).Error
}

// DeleteExpiredSessions prunes the user's expired sessions on login.
func (r *Repository) DeleteExpiredSessions(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and expires_at <= now()", userID).
		Delete(&SessionRow{}).Error
}

// FindAPITokenUser resolves the owner of a hashed personal access token.
func (r *Repository) FindAPITokenUser(ctx context.Context, tokenHash string) (*string, error) {
	var userID string
	result := r.db.WithContext(ctx).
		Raw("select user_id from api_tokens where token_hash = ?", tokenHash).
		Scan(&userID)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return &userID, nil
}

// TouchAPIToken refreshes `last_used_at`; failures are never fatal.
func (r *Repository) TouchAPIToken(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).
		Exec("update api_tokens set last_used_at = now() where token_hash = ?", tokenHash).Error
}
