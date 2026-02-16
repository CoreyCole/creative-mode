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

// DeleteUserMessages removes all conversation messages for a given user.
func (s *SQLiteMessageStore) DeleteUserMessages(userID string) error {
	_, err := s.db.Exec(`DELETE FROM conversation_messages WHERE discord_id = ?`, userID)
	return err
}

// AddImage inserts an image record into the conversation_images table.
func (s *SQLiteMessageStore) AddImage(discordID string, messageIndex int, imageID, filePath, mimeType, filename string) error {
	_, err := s.db.Exec(
		`INSERT INTO conversation_images (discord_id, message_index, image_id, file_path, mime_type, original_filename) VALUES (?, ?, ?, ?, ?, ?)`,
		discordID, messageIndex, imageID, filePath, mimeType, filename,
	)
	return err
}

// GetImages returns all image records for a user, ordered by id.
func (s *SQLiteMessageStore) GetImages(discordID string) ([]mayorchat.ImageRecord, error) {
	rows, err := s.db.Query(
		`SELECT discord_id, message_index, image_id, file_path, mime_type, original_filename FROM conversation_images WHERE discord_id = ? ORDER BY id ASC`,
		discordID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []mayorchat.ImageRecord
	for rows.Next() {
		var r mayorchat.ImageRecord
		if scanErr := rows.Scan(&r.DiscordID, &r.MessageIndex, &r.ImageID, &r.FilePath, &r.MIMEType, &r.Filename); scanErr != nil {
			continue
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetImageByID returns a single image record by its unique image ID.
func (s *SQLiteMessageStore) GetImageByID(imageID string) (*mayorchat.ImageRecord, error) {
	var r mayorchat.ImageRecord
	err := s.db.QueryRow(
		`SELECT discord_id, message_index, image_id, file_path, mime_type, original_filename FROM conversation_images WHERE image_id = ?`,
		imageID,
	).Scan(&r.DiscordID, &r.MessageIndex, &r.ImageID, &r.FilePath, &r.MIMEType, &r.Filename)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteImages removes all image records for a user.
func (s *SQLiteMessageStore) DeleteImages(discordID string) error {
	_, err := s.db.Exec(`DELETE FROM conversation_images WHERE discord_id = ?`, discordID)
	return err
}

// DeleteImagesOlderThan removes image records older than the given duration
// and returns the file paths of deleted rows for cleanup.
func (s *SQLiteMessageStore) DeleteImagesOlderThan(d time.Duration) ([]string, error) {
	rows, err := s.db.Query(`SELECT file_path FROM conversation_images WHERE created_at < datetime('now', '-24 hours')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var p string
		if scanErr := rows.Scan(&p); scanErr != nil {
			continue
		}
		paths = append(paths, p)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	_, err = s.db.Exec(`DELETE FROM conversation_images WHERE created_at < datetime('now', '-24 hours')`)
	return paths, err
}
