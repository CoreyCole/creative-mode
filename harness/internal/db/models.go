package db

import (
	"database/sql"
	"time"
)

// User represents a GitHub-authenticated user.
type User struct {
	ID             string
	GitHubID       int64
	GitHubUsername string
	AvatarURL      sql.NullString
	Role           string // "admin", "user", "pending"
	CreatedAt      time.Time
	LastSeenAt     time.Time
}

// Session represents an authenticated browser session.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// World represents a game world that players can join.
type World struct {
	ID          string
	Name        string
	Description sql.NullString
	CreatedBy   sql.NullString
	CreatedAt   time.Time
}

// Checkpoint represents a snapshot of a world's game code at a point in time.
type Checkpoint struct {
	ID                 string
	WorldID            string
	ParentCheckpointID sql.NullString
	Name               sql.NullString
	Prompt             sql.NullString
	Status             string // "building", "ready", "failed"
	BuildLog           sql.NullString
	WorkSummary        sql.NullString
	FilesChanged       sql.NullString // JSON array of file paths
	BuildDurationMs    sql.NullInt64
	DirPath            string
	WasmPath           sql.NullString
	ServerPort         sql.NullInt64
	CreatedBy          sql.NullString
	CreatedAt          time.Time
}

// Message represents a chat message or system notification.
type Message struct {
	ID           string
	Type         string // "chat", "build.started", "build.completed", "build.failed", "player.joined", "player.left"
	UserID       sql.NullString
	WorldID      sql.NullString
	CheckpointID sql.NullString
	Content      string
	CreatedAt    time.Time
}
