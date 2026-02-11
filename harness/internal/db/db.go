package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB wraps a sql.DB connection with application-specific query methods.
type DB struct {
	db *sql.DB
}

// New opens a SQLite database at the given path, enables WAL mode and
// busy_timeout, and runs all embedded migrations.
func New(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// WAL mode for concurrent access from multiple goroutines.
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	// 5-second busy timeout to avoid "database is locked" errors.
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	d := &DB{db: sqlDB}
	if err := d.runMigrations(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// runMigrations executes all embedded SQL migration files.
func (d *DB) runMigrations() error {
	content, err := migrations.ReadFile("migrations/001_initial.sql")
	if err != nil {
		return fmt.Errorf("reading migration file: %w", err)
	}
	if _, err := d.db.Exec(string(content)); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}
	return nil
}
