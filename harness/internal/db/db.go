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

// runMigrations executes all embedded SQL migration files in order,
// skipping any that have already been applied.
func (d *DB) runMigrations(ctx context.Context) error {
	// Create migration tracking table if it doesn't exist.
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		name TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	// Bootstrap: for any migration that's not tracked but whose effects
	// already exist in the schema, mark it as applied to avoid re-running.
	d.bootstrapExistingMigrations(ctx)

	migrationFiles := []string{
		"migrations/001_initial.sql",
		"migrations/002_cascades_indexes.sql",
		"migrations/003_template_type.sql",
		"migrations/004_mayor_and_instrumentation.sql",
		"migrations/005_cover_image.sql",
	}
	for _, file := range migrationFiles {
		// Check if already applied.
		var count int
		if err := d.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM _migrations WHERE name = ?", file,
		).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %s: %w", file, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrations.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if _, err := d.db.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("executing %s: %w", file, err)
		}

		// Record as applied.
		if _, err := d.db.ExecContext(ctx,
			"INSERT INTO _migrations (name) VALUES (?)", file,
		); err != nil {
			return fmt.Errorf("recording migration %s: %w", file, err)
		}
	}

	return nil
}

// bootstrapExistingMigrations checks each migration's schema effects and marks
// it as applied if the schema already reflects the change. This handles the
// case where migrations ran before tracking was introduced, or where the app
// crashed mid-bootstrap leaving partial tracking state.
func (d *DB) bootstrapExistingMigrations(ctx context.Context) {
	// 001: creates the worlds table (and others).
	var worldsExist int
	_ = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='worlds'",
	).Scan(&worldsExist)
	if worldsExist > 0 {
		_, _ = d.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
			"migrations/001_initial.sql")
	}

	// 002: creates indexes (idempotent, but mark it if tables exist).
	if worldsExist > 0 {
		_, _ = d.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
			"migrations/002_cascades_indexes.sql")
	}

	// 003: adds template_type column to worlds.
	var hasCol int
	_ = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('worlds') WHERE name='template_type'",
	).Scan(&hasCol)
	if hasCol > 0 {
		_, _ = d.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
			"migrations/003_template_type.sql")
	}

	// 004: adds mayor_name column to worlds + mayor tables.
	var hasMayorCol int
	_ = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('worlds') WHERE name='mayor_name'",
	).Scan(&hasMayorCol)
	if hasMayorCol > 0 {
		_, _ = d.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
			"migrations/004_mayor_and_instrumentation.sql")
	}

	// 005: adds cover_image_path column to worlds.
	var hasCoverCol int
	_ = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('worlds') WHERE name='cover_image_path'",
	).Scan(&hasCoverCol)
	if hasCoverCol > 0 {
		_, _ = d.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO _migrations (name) VALUES (?)",
			"migrations/005_cover_image.sql")
	}
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
