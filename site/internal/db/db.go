package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// New opens (or creates) a SQLite database at dbPath and ensures the schema
// is up to date. WAL mode + busy timeout are set via DSN pragmas.
func New(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := createTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			discord_id TEXT NOT NULL,
			discord_username TEXT NOT NULL,
			discord_avatar TEXT NOT NULL DEFAULT '',
			guild_member_verified INTEGER NOT NULL DEFAULT 0,
			invite_code_verified INTEGER NOT NULL DEFAULT 0,
			system_prompt TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_discord_id ON sessions(discord_id);

		CREATE TABLE IF NOT EXISTS conversation_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			discord_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_conv_messages_discord_id ON conversation_messages(discord_id);

		CREATE TABLE IF NOT EXISTS page_views (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			visitor_hash TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_page_views_created_at ON page_views(created_at);

		CREATE TABLE IF NOT EXISTS metrics_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mem_used_percent REAL NOT NULL,
			mem_used_bytes INTEGER NOT NULL DEFAULT 0,
			total_visits INTEGER NOT NULL,
			unique_visitors INTEGER NOT NULL DEFAULT 0,
			discord_members INTEGER NOT NULL DEFAULT 0,
			worlds_hatched INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_metrics_created_at ON metrics_snapshots(created_at);
	`)
	if err != nil {
		return err
	}

	// Migrations for columns added after initial schema.
	_, _ = db.Exec("ALTER TABLE metrics_snapshots ADD COLUMN mem_used_bytes INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE page_views ADD COLUMN visitor_hash TEXT NOT NULL DEFAULT ''")
	_, _ = db.Exec("ALTER TABLE metrics_snapshots ADD COLUMN unique_visitors INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE metrics_snapshots ADD COLUMN discord_members INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE metrics_snapshots ADD COLUMN worlds_hatched INTEGER NOT NULL DEFAULT 0")
	return nil
}
