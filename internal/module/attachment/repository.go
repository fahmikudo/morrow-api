package attachment

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Repository is the only place attachment SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the attachment repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// EntityExists reports whether the user owns the referenced project, task, or note.
func (r *Repository) EntityExists(ctx context.Context, userID, entityType, entityID string) (bool, error) {
	table := map[string]string{
		EntityProject: "projects",
		EntityTask:    "tasks",
		EntityNote:    "notes",
	}[entityType]
	if table == "" {
		return false, nil
	}

	var exists bool
	// The table name comes from the closed map above, never from the request.
	query := "select exists (select 1 from " + table + " where id = ? and user_id = ?)"
	if err := r.db.WithContext(ctx).Raw(query, entityID, userID).Scan(&exists).Error; err != nil {
		return false, err
	}
	return exists, nil
}

// Insert stores pending metadata before the browser uploads.
func (r *Repository) Insert(ctx context.Context, row Row) error {
	return r.db.WithContext(ctx).Create(&row).Error
}

// Get returns one owned attachment, including pending rows.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Row, error) {
	var row Row
	err := r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListReady returns uploaded files for one entity, newest first.
func (r *Repository) ListReady(
	ctx context.Context,
	userID, entityType, entityID string,
) ([]Row, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and entity_type = ? and entity_id = ? and status = 'ready'", userID, entityType, entityID).
		Order("created_at desc, id desc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListForEntity returns every row for one entity, including pending rows.
func (r *Repository) ListForEntity(
	ctx context.Context,
	userID, entityType, entityID string,
) ([]Row, error) {
	rows := make([]Row, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? and entity_type = ? and entity_id = ?", userID, entityType, entityID).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkReady flips a pending upload to ready with the object's actual size.
func (r *Repository) MarkReady(ctx context.Context, userID, id string, size int64) (bool, error) {
	result := r.db.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ? and status = 'pending'", id, userID).
		Updates(map[string]any{"status": "ready", "byte_size": size})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// Delete removes one owned row.
func (r *Repository) Delete(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{}).Error
}

// DeleteByEntity removes every row for a project, task, or note.
func (r *Repository) DeleteByEntity(ctx context.Context, userID, entityType, entityID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? and entity_type = ? and entity_id = ?", userID, entityType, entityID).
		Delete(&Row{}).Error
}

// StalePending returns abandoned uploads older than the cutoff.
func (r *Repository) StalePending(ctx context.Context, userID string, before time.Time, limit int) ([]Row, error) {
	rows := make([]Row, 0)
	query := r.db.WithContext(ctx).
		Where("user_id = ? and status = 'pending' and created_at < ?", userID, before)
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
