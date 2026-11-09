// Command migrate applies the Goose migrations in ./migrations.
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"morrow-api/internal/config"
	"morrow-api/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: migrate <up|down|status>")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("could not connect to PostgreSQL using DATABASE_URL (%w)", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	switch os.Args[1] {
	case "up":
		return goose.Up(db, ".")
	case "down":
		return goose.DownTo(db, ".", 0)
	case "status":
		return goose.Status(db, ".")
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
