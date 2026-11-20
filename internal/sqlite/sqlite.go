// Package sqlite opens and migrates the application's SQLite database.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

const busyTimeoutMilliseconds = 5000

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Open opens a SQLite database, applies safe connection settings, and runs all
// pending embedded migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("SQLite path is empty")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	// PRAGMAs are connection-local. A single connection ensures every query
	// uses the configured connection and also avoids SQLite write contention.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := configure(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func configure(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping SQLite database: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable SQLite foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMilliseconds)); err != nil {
		return fmt.Errorf("set SQLite busy timeout: %w", err)
	}

	// In-memory databases and some filesystems cannot use WAL. SQLite returns
	// the mode it can support, so no particular returned mode is required.
	var journalMode string
	_ = db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode)

	if _, err := db.ExecContext(ctx, "PRAGMA synchronous = NORMAL"); err != nil {
		return fmt.Errorf("set SQLite synchronous mode: %w", err)
	}

	var foreignKeys int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("verify SQLite foreign keys: %w", err)
	}
	if foreignKeys != 1 {
		return errors.New("SQLite foreign keys are disabled")
	}

	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list SQLite migrations: %w", err)
	}
	sort.Strings(entries)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite migration transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create SQLite migration table: %w", err)
	}

	for _, entry := range entries {
		version := filepath.Base(entry)

		var applied int
		err := tx.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?",
			version,
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check SQLite migration %s: %w", version, err)
		}
		if applied != 0 {
			continue
		}

		script, err := migrationFiles.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("read SQLite migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("apply SQLite migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO schema_migrations (version) VALUES (?)",
			version,
		); err != nil {
			return fmt.Errorf("record SQLite migration %s: %w", version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite migrations: %w", err)
	}

	return nil
}
