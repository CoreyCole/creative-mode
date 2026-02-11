package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"creative-mode/harness/internal/db/sqlc"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB wraps a sql.DB connection and embeds SQLc-generated query methods.
type DB struct {
	*sqlc.Queries
	db *sql.DB
}

// New opens a SQLite database at the given path, enables WAL mode and
// busy_timeout, and runs all embedded migrations.
func New(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite performs best with a single writer connection.
	sqlDB.SetMaxOpenConns(1)

	ctx := context.Background()

	// WAL mode for concurrent access from multiple goroutines.
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	// 5-second busy timeout to avoid "database is locked" errors.
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	d := &DB{
		Queries: sqlc.New(sqlDB),
		db:      sqlDB,
	}
	if err := d.runMigrations(ctx); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// BeginTx starts a new transaction.
func (d *DB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, nil)
}

// WithTx returns a Queries instance scoped to the given transaction.
func (d *DB) WithTx(tx *sql.Tx) *sqlc.Queries {
	return d.Queries.WithTx(tx)
}

// runMigrations executes all embedded SQL migration files in order.
func (d *DB) runMigrations(ctx context.Context) error {
	migrationFiles := []string{
		"migrations/001_initial.sql",
		"migrations/002_cascades_indexes.sql",
	}
	for _, file := range migrationFiles {
		content, err := migrations.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if _, err := d.db.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("executing %s: %w", file, err)
		}
	}

	return nil
}

// GetCheckpointAncestry returns the chain of checkpoints from the given
// checkpoint back to the root (the checkpoint with no parent), ordered from
// root to the given checkpoint.
func (d *DB) GetCheckpointAncestry(
	ctx context.Context,
	worldID, cpID string,
) ([]sqlc.Checkpoint, error) {
	allCPs, err := d.GetCheckpointTree(ctx, worldID)
	if err != nil {
		return nil, err
	}

	cpMap := make(map[string]sqlc.Checkpoint, len(allCPs))
	for _, cp := range allCPs {
		cpMap[cp.ID] = cp
	}

	visited := make(map[string]bool, len(cpMap))
	var ancestry []sqlc.Checkpoint
	currentID := cpID
	for currentID != "" {
		if visited[currentID] {
			return nil, fmt.Errorf(
				"cycle detected at checkpoint %s in world %s",
				currentID,
				worldID,
			)
		}
		visited[currentID] = true
		cp, ok := cpMap[currentID]
		if !ok {
			return nil, fmt.Errorf(
				"checkpoint %s not found in world %s",
				currentID,
				worldID,
			)
		}
		ancestry = append(ancestry, cp)
		if cp.ParentCheckpointID.Valid {
			currentID = cp.ParentCheckpointID.String
		} else {
			currentID = ""
		}
	}

	// Reverse to get root-first order.
	for i, j := 0, len(ancestry)-1; i < j; i, j = i+1, j-1 {
		ancestry[i], ancestry[j] = ancestry[j], ancestry[i]
	}

	return ancestry, nil
}
