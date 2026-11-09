// Package database opens the PostgreSQL pool and scopes transactions.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"morrow-api/internal/config"
)

type slogWriter struct {
	log *slog.Logger
}

// Printf adapts Gorm's logger interface to slog.
func (w slogWriter) Printf(format string, args ...any) {
	w.log.Error(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// Open connects to DATABASE_URL. Schema changes belong to the SQL migrations,
// never to AutoMigrate.
func Open(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gorm.DB, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
		Logger: gormlogger.New(slogWriter{log: log}, gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Error,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("could not connect to PostgreSQL using DATABASE_URL (%w)", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("could not connect to PostgreSQL using DATABASE_URL (%w)", err)
	}

	return db, nil
}

// Tx runs fn inside one transaction and commits only when fn returns nil.
func Tx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(fn)
}
