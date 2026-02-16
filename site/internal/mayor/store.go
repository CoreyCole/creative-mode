package mayor

import (
	"database/sql"
	"time"

	"github.com/coreycole/creative-mode/pkg/mayorchat"
)

// SQLiteMessageStore implements mayorchat.MessageStore using SQLite.
type SQLiteMessageStore struct {
	db *sql.DB
}

// NewSQLiteMessageStore creates a new SQLite-backed message store.
func NewSQLiteMessageStore(db *sql.DB) *SQLiteMessageStore {
	return &SQLiteMessageStore{db: db}
}

// AddMessage inserts a message into the conversation_messages table.
func (s *SQLiteMessageStore) AddMessage(userID, role, content string) error {
	_, err := s.db.Exec(
		`INSERT INTO conversation_messages (discord_id, role, content) VALUES (?, ?, ?)`,
		userID, role, content,
	)
	return err
}

// GetMessages returns the conversation history for a user.
func (s *SQLiteMessageStore) GetMessages(userID string) ([]mayorchat.Message, error) {
	rows, err := s.db.Query(
		`SELECT role, content FROM conversation_messages WHERE discord_id = ? ORDER BY id ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []mayorchat.Message
	for rows.Next() {
		var m mayorchat.Message
		if scanErr := rows.Scan(&m.Role, &m.Content); scanErr != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// DeleteOlderThan removes messages older than the given duration.
func (s *SQLiteMessageStore) DeleteOlderThan(_ time.Duration) error {
	_, err := s.db.Exec(`DELETE FROM conversation_messages WHERE created_at < datetime('now', '-24 hours')`)
	return err
}
