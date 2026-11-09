package inbox

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the only place inbox_items SQL lives.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds the inbox repository on the shared pool.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListActive returns the items that are not archived.
func (r *Repository) ListActive(ctx context.Context, userID string) ([]Item, error) {
	return r.list(ctx, userID, "archived_at is null")
}

// ListArchived returns the archived items.
func (r *Repository) ListArchived(ctx context.Context, userID string) ([]Item, error) {
	return r.list(ctx, userID, "archived_at is not null")
}

// ListAll returns every item, archived included (snapshot and import paths).
func (r *Repository) ListAll(ctx context.Context, userID string) ([]Item, error) {
	return r.list(ctx, userID, "")
}

// Get returns an item or nil.
func (r *Repository) Get(ctx context.Context, userID, id string) (*Item, error) {
	row, err := r.find(ctx, r.db, userID, id, false)
	if err != nil || row == nil {
		return nil, err
	}

	item := row.ToWire()
	return &item, nil
}

// GetForUpdate locks the item row inside a transaction.
func (r *Repository) GetForUpdate(ctx context.Context, tx *gorm.DB, userID, id string) (*Row, error) {
	return r.find(ctx, tx, userID, id, true)
}

// Insert writes an inbox row inside the caller's transaction.
func (r *Repository) Insert(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Create(&row).Error
}

// Save writes the mutable inbox columns back; `created_at` never changes.
func (r *Repository) Save(ctx context.Context, tx *gorm.DB, row Row) error {
	return tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? and user_id = ?", row.ID, row.UserID).
		Updates(map[string]any{
			"title":       row.Title,
			"content":     row.Content,
			"type":        row.Type,
			"archived_at": row.ArchivedAt,
		}).Error
}

// Delete removes an inbox item.
func (r *Repository) Delete(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? and user_id = ?", id, userID).
		Delete(&Row{}).Error
}

func (r *Repository) list(ctx context.Context, userID, filter string) ([]Item, error) {
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if filter != "" {
		query = query.Where(filter)
	}

	rows := make([]Row, 0)
	if err := query.Order("created_at asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.ToWire())
	}

	return items, nil
}

func (r *Repository) find(ctx context.Context, runner *gorm.DB, userID, id string, forUpdate bool) (*Row, error) {
	query := runner.WithContext(ctx).Where("id = ? and user_id = ?", id, userID)
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}

	var row Row
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &row, nil
}
