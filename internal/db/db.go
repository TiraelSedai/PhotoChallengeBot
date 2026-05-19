package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type Options struct {
	Path          string
	MigrationsDir string
	BusyTimeout   int
}

var gooseMu sync.Mutex

func Open(ctx context.Context, opts Options) (*sqlx.DB, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if opts.MigrationsDir == "" {
		return nil, fmt.Errorf("migrations dir is required")
	}
	if opts.BusyTimeout == 0 {
		opts.BusyTimeout = 5000
	}

	database, err := sqlx.Open("sqlite", opts.Path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := configure(ctx, database, opts.BusyTimeout); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := migrate(database.DB, opts.MigrationsDir); err != nil {
		_ = database.Close()
		return nil, err
	}

	return database, nil
}

func configure(ctx context.Context, database *sqlx.DB, busyTimeout int) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout),
		"PRAGMA foreign_keys = ON",
	}

	for _, pragma := range pragmas {
		if _, err := database.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return nil
}

func migrate(database *sql.DB, migrationsDir string) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(database, migrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
