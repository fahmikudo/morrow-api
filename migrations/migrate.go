package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
)

// Up applies every pending migration to db.
func Up(db *sql.DB) (err error) {
	migration, err := open(db)
	if err != nil {
		return err
	}
	defer closeMigration(migration, &err)

	if err = migration.Up(); errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

// Down rolls db back to the empty schema.
func Down(db *sql.DB) (err error) {
	migration, err := open(db)
	if err != nil {
		return err
	}
	defer closeMigration(migration, &err)

	if err = migration.Down(); errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

// Status returns the applied migration version and whether it is dirty.
// A nil version means no migration has been applied.
func Status(db *sql.DB) (version *uint, dirty bool, err error) {
	migration, err := open(db)
	if err != nil {
		return nil, false, err
	}
	defer closeMigration(migration, &err)

	current, dirty, err := migration.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &current, dirty, nil
}

func open(db *sql.DB) (*migrate.Migrate, error) {
	sourceDriver, err := httpfs.New(http.FS(FS), ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}

	databaseDriver, err := postgres.WithInstance(db, &postgres.Config{MultiStatementEnabled: true})
	if err != nil {
		_ = sourceDriver.Close()
		return nil, fmt.Errorf("open PostgreSQL migration driver: %w", err)
	}

	migration, err := migrate.NewWithInstance("httpfs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = sourceDriver.Close()
		_ = databaseDriver.Close()
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return migration, nil
}

func closeMigration(migration *migrate.Migrate, result *error) {
	sourceErr, databaseErr := migration.Close()
	if *result == nil {
		*result = errors.Join(sourceErr, databaseErr)
	}
}
