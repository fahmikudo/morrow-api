// Command migrate applies the embedded golang-migrate migrations.
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

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

	switch os.Args[1] {
	case "up":
		return migrations.Up(db)
	case "down":
		return migrations.Down(db)
	case "status":
		version, dirty, err := migrations.Status(db)
		if err != nil {
			return err
		}
		if version == nil {
			fmt.Println("version: none\ndirty: false")
			return nil
		}
		fmt.Printf("version: %d\ndirty: %t\n", *version, dirty)
		return nil
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
