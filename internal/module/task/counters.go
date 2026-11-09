package task

import (
	"context"

	"gorm.io/gorm"
)

// Counters allocates the per-bucket task numbers. The three statements need
// `RETURNING` and `greatest`, so they stay raw SQL.
type Counters struct {
	db *gorm.DB
}

// NewCounters builds the counter store on the shared pool.
func NewCounters(db *gorm.DB) *Counters {
	return &Counters{db: db}
}

// Next atomically reserves the next number in a bucket and returns it.
func (c *Counters) Next(ctx context.Context, tx *gorm.DB, userID, bucket string) (int32, error) {
	row := tx.WithContext(ctx).Raw(
		`insert into counters (user_id, bucket, next) values (?, ?, 1)
		 on conflict (user_id, bucket) do update set next = counters.next + 1
		 returning next`, userID, bucket,
	).Row()

	var next int32
	if err := row.Scan(&next); err != nil {
		return 0, err
	}

	return next, nil
}

// Set keeps the larger value so an import never rewinds a bucket.
func (c *Counters) Set(ctx context.Context, tx *gorm.DB, userID, bucket string, next int32) error {
	if next < 0 {
		next = 0
	}

	return tx.WithContext(ctx).Exec(
		`insert into counters (user_id, bucket, next) values (?, ?, ?)
		 on conflict (user_id, bucket) do update set next = greatest(counters.next, excluded.next)`,
		userID, bucket, next,
	).Error
}

// List returns every bucket, ordered by name.
func (c *Counters) List(ctx context.Context, userID string) (map[string]int32, error) {
	type counterRow struct {
		Bucket string `gorm:"column:bucket"`
		Next   int32  `gorm:"column:next"`
	}

	rows := make([]counterRow, 0)
	if err := c.db.WithContext(ctx).Raw(
		"select bucket, next from counters where user_id = ? order by bucket asc", userID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}

	counters := make(map[string]int32, len(rows))
	for _, row := range rows {
		counters[row.Bucket] = row.Next
	}

	return counters, nil
}
