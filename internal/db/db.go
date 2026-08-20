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

	// Pragmas are per-connection, and database/sql opens connections whenever it feels like it,
	// so they belong in the DSN rather than in a one-off Exec after Open. No mmap_size: the host
	// has 2 GB of RAM and no swap.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(ON)&_pragma=cache_size(-20000)&_pragma=synchronous(NORMAL)",
		opts.Path,
		opts.BusyTimeout,
	)
	database, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := migrate(ctx, database.DB, opts.MigrationsDir); err != nil {
		_ = database.Close()
		return nil, err
	}

	return database, nil
}

func migrate(ctx context.Context, database *sql.DB, migrationsDir string) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, database, migrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
