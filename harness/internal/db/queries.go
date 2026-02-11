package db

import (
	"database/sql"
	"fmt"
	"time"
)

// --- Users ---

// UpsertUser inserts a new user or updates an existing one (matched by github_id).
func (d *DB) UpsertUser(id string, githubID int64, username string, avatarURL sql.NullString, role string) error {
	_, err := d.db.Exec(`
		INSERT INTO users (id, github_id, github_username, avatar_url, role)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(github_id) DO UPDATE SET
			github_username = excluded.github_username,
			avatar_url = excluded.avatar_url,
			last_seen_at = CURRENT_TIMESTAMP
	`, id, githubID, username, avatarURL, role)
	return err
}

// GetUserByID retrieves a user by their internal ID.
func (d *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := d.db.QueryRow(`
		SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.GitHubID, &u.GitHubUsername, &u.AvatarURL, &u.Role, &u.CreatedAt, &u.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByGitHubID retrieves a user by their GitHub ID.
func (d *DB) GetUserByGitHubID(githubID int64) (*User, error) {
	u := &User{}
	err := d.db.QueryRow(`
		SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
		FROM users WHERE github_id = ?
	`, githubID).Scan(&u.ID, &u.GitHubID, &u.GitHubUsername, &u.AvatarURL, &u.Role, &u.CreatedAt, &u.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateUserRole updates a user's role (admin, user, pending).
func (d *DB) UpdateUserRole(id string, role string) error {
	result, err := d.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("user %s not found", id)
	}
	return nil
}

// ListUsers returns all users ordered by creation time.
func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.db.Query(`
		SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
		FROM users ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.GitHubID, &u.GitHubUsername, &u.AvatarURL, &u.Role, &u.CreatedAt, &u.LastSeenAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ListPendingUsers returns all users with the "pending" role.
func (d *DB) ListPendingUsers() ([]User, error) {
	rows, err := d.db.Query(`
		SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
		FROM users WHERE role = 'pending' ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.GitHubID, &u.GitHubUsername, &u.AvatarURL, &u.Role, &u.CreatedAt, &u.LastSeenAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser removes a user by ID.
func (d *DB) DeleteUser(id string) error {
	_, err := d.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// CountUsers returns the total number of users in the database.
func (d *DB) CountUsers() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// UpdateLastSeen updates the last_seen_at timestamp for a user.
func (d *DB) UpdateLastSeen(id string) error {
	_, err := d.db.Exec(`UPDATE users SET last_seen_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// --- Sessions ---

// CreateSession inserts a new session.
func (d *DB) CreateSession(id string, userID string, expiresAt time.Time) error {
	_, err := d.db.Exec(`
		INSERT INTO sessions (id, user_id, expires_at)
		VALUES (?, ?, ?)
	`, id, userID, expiresAt)
	return err
}

// GetSession retrieves a session by ID. Returns nil if the session does not
// exist or has expired.
func (d *DB) GetSession(id string) (*Session, error) {
	s := &Session{}
	err := d.db.QueryRow(`
		SELECT id, user_id, created_at, expires_at
		FROM sessions WHERE id = ? AND expires_at > CURRENT_TIMESTAMP
	`, id).Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// DeleteSession removes a session by ID.
func (d *DB) DeleteSession(id string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteExpiredSessions removes all sessions that have passed their expiry.
func (d *DB) DeleteExpiredSessions() error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP`)
	return err
}

// --- Worlds ---

// CreateWorld inserts a new world.
func (d *DB) CreateWorld(w *World) error {
	_, err := d.db.Exec(`
		INSERT INTO worlds (id, name, description, created_by)
		VALUES (?, ?, ?, ?)
	`, w.ID, w.Name, w.Description, w.CreatedBy)
	return err
}

// GetWorld retrieves a world by ID. Returns nil if not found.
func (d *DB) GetWorld(id string) (*World, error) {
	w := &World{}
	err := d.db.QueryRow(`
		SELECT id, name, description, created_by, created_at
		FROM worlds WHERE id = ?
	`, id).Scan(&w.ID, &w.Name, &w.Description, &w.CreatedBy, &w.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

// ListWorlds returns all worlds ordered by creation time.
func (d *DB) ListWorlds() ([]World, error) {
	rows, err := d.db.Query(`
		SELECT id, name, description, created_by, created_at
		FROM worlds ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worlds []World
	for rows.Next() {
		var w World
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.CreatedBy, &w.CreatedAt); err != nil {
			return nil, err
		}
		worlds = append(worlds, w)
	}
	return worlds, rows.Err()
}

// --- Checkpoints ---

// CreateCheckpoint inserts a new checkpoint.
func (d *DB) CreateCheckpoint(cp *Checkpoint) error {
	_, err := d.db.Exec(`
		INSERT INTO checkpoints (id, world_id, parent_checkpoint_id, name, prompt, status, dir_path, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, cp.ID, cp.WorldID, cp.ParentCheckpointID, cp.Name, cp.Prompt, cp.Status, cp.DirPath, cp.CreatedBy)
	return err
}

// GetCheckpoint retrieves a checkpoint by ID. Returns nil if not found.
func (d *DB) GetCheckpoint(id string) (*Checkpoint, error) {
	cp := &Checkpoint{}
	err := d.db.QueryRow(`
		SELECT id, world_id, parent_checkpoint_id, name, prompt, status,
		       build_log, work_summary, files_changed, build_duration_ms,
		       dir_path, wasm_path, server_port, created_by, created_at
		FROM checkpoints WHERE id = ?
	`, id).Scan(
		&cp.ID, &cp.WorldID, &cp.ParentCheckpointID, &cp.Name, &cp.Prompt, &cp.Status,
		&cp.BuildLog, &cp.WorkSummary, &cp.FilesChanged, &cp.BuildDurationMs,
		&cp.DirPath, &cp.WasmPath, &cp.ServerPort, &cp.CreatedBy, &cp.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cp, nil
}

// UpdateCheckpointStatus updates a checkpoint's status and build log.
func (d *DB) UpdateCheckpointStatus(id string, status string, buildLog string) error {
	result, err := d.db.Exec(`
		UPDATE checkpoints SET status = ?, build_log = ? WHERE id = ?
	`, status, buildLog, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("checkpoint %s not found", id)
	}
	return nil
}

// UpdateCheckpointSummary updates a checkpoint's work summary, files changed,
// and build duration after a successful build.
func (d *DB) UpdateCheckpointSummary(id string, workSummary string, filesChanged string, buildDurationMs int64) error {
	result, err := d.db.Exec(`
		UPDATE checkpoints SET work_summary = ?, files_changed = ?, build_duration_ms = ? WHERE id = ?
	`, workSummary, filesChanged, buildDurationMs, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("checkpoint %s not found", id)
	}
	return nil
}

// UpdateCheckpointServerPort sets the game server port for a checkpoint.
func (d *DB) UpdateCheckpointServerPort(id string, port int) error {
	result, err := d.db.Exec(`
		UPDATE checkpoints SET server_port = ? WHERE id = ?
	`, port, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("checkpoint %s not found", id)
	}
	return nil
}

// GetCheckpointTree returns all checkpoints for a given world, ordered by creation time.
func (d *DB) GetCheckpointTree(worldID string) ([]Checkpoint, error) {
	rows, err := d.db.Query(`
		SELECT id, world_id, parent_checkpoint_id, name, prompt, status,
		       build_log, work_summary, files_changed, build_duration_ms,
		       dir_path, wasm_path, server_port, created_by, created_at
		FROM checkpoints WHERE world_id = ? ORDER BY created_at ASC
	`, worldID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checkpoints []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		if err := rows.Scan(
			&cp.ID, &cp.WorldID, &cp.ParentCheckpointID, &cp.Name, &cp.Prompt, &cp.Status,
			&cp.BuildLog, &cp.WorkSummary, &cp.FilesChanged, &cp.BuildDurationMs,
			&cp.DirPath, &cp.WasmPath, &cp.ServerPort, &cp.CreatedBy, &cp.CreatedAt,
		); err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, cp)
	}
	return checkpoints, rows.Err()
}

// GetCheckpointAncestry returns the chain of checkpoints from the given
// checkpoint back to the root (the checkpoint with no parent), ordered from
// root to the given checkpoint.
func (d *DB) GetCheckpointAncestry(worldID string, cpID string) ([]Checkpoint, error) {
	// First, get all checkpoints in this world and build a parent map in memory.
	// This is simpler and more portable than recursive CTEs.
	allCPs, err := d.GetCheckpointTree(worldID)
	if err != nil {
		return nil, err
	}

	// Build a map for quick lookup.
	cpMap := make(map[string]Checkpoint, len(allCPs))
	for _, cp := range allCPs {
		cpMap[cp.ID] = cp
	}

	// Walk from the given checkpoint to the root.
	var ancestry []Checkpoint
	currentID := cpID
	for currentID != "" {
		cp, ok := cpMap[currentID]
		if !ok {
			return nil, fmt.Errorf("checkpoint %s not found in world %s", currentID, worldID)
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

// --- User Positions ---

// GetUserPosition returns the checkpoint ID for a user's position in a world.
// Returns empty string and nil error if no position is set.
func (d *DB) GetUserPosition(userID string, worldID string) (string, error) {
	var checkpointID string
	err := d.db.QueryRow(`
		SELECT checkpoint_id FROM user_positions
		WHERE user_id = ? AND world_id = ?
	`, userID, worldID).Scan(&checkpointID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return checkpointID, nil
}

// SetUserPosition sets or updates a user's current checkpoint in a world.
func (d *DB) SetUserPosition(userID string, worldID string, checkpointID string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_positions (user_id, world_id, checkpoint_id, last_accessed_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, world_id) DO UPDATE SET
			checkpoint_id = excluded.checkpoint_id,
			last_accessed_at = CURRENT_TIMESTAMP
	`, userID, worldID, checkpointID)
	return err
}

// --- Prompt History ---

// CreatePromptHistory records a prompt submission.
func (d *DB) CreatePromptHistory(id string, cpID string, worldID string, userID string, promptText string) error {
	_, err := d.db.Exec(`
		INSERT INTO prompt_history (id, checkpoint_id, world_id, user_id, prompt_text)
		VALUES (?, ?, ?, ?, ?)
	`, id, cpID, worldID, userID, promptText)
	return err
}

// --- Messages ---

// CreateMessage inserts a new message (chat or system notification).
func (d *DB) CreateMessage(msg *Message) error {
	_, err := d.db.Exec(`
		INSERT INTO messages (id, type, user_id, world_id, checkpoint_id, content)
		VALUES (?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.Type, msg.UserID, msg.WorldID, msg.CheckpointID, msg.Content)
	return err
}

// GetRecentMessages returns the most recent messages globally, ordered by
// created_at DESC (newest first).
func (d *DB) GetRecentMessages(limit int) ([]Message, error) {
	rows, err := d.db.Query(`
		SELECT id, type, user_id, world_id, checkpoint_id, content, created_at
		FROM messages ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Type, &m.UserID, &m.WorldID, &m.CheckpointID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// GetRecentMessagesByWorld returns the most recent messages for a specific
// world, ordered by created_at DESC (newest first).
func (d *DB) GetRecentMessagesByWorld(worldID string, limit int) ([]Message, error) {
	rows, err := d.db.Query(`
		SELECT id, type, user_id, world_id, checkpoint_id, content, created_at
		FROM messages WHERE world_id = ? ORDER BY created_at DESC LIMIT ?
	`, worldID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Type, &m.UserID, &m.WorldID, &m.CheckpointID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
