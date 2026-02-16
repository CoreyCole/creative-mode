package mayorchat

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// MessageStore is the persistence interface for conversation messages.
type MessageStore interface {
	AddMessage(userID, role, content string) error
	GetMessages(userID string) ([]Message, error)
	DeleteOlderThan(d time.Duration) error
	DeleteUserMessages(userID string) error
}

// ImageStore is the persistence interface for conversation image attachments.
type ImageStore interface {
	AddImage(discordID string, messageIndex int, imageID, filePath, mimeType, filename string) error
	GetImages(discordID string) ([]ImageRecord, error)
	GetImageByID(imageID string) (*ImageRecord, error)
	DeleteImages(discordID string) error
	DeleteImagesOlderThan(d time.Duration) ([]string, error) // returns file paths of deleted rows
}

// ImageRecord represents a persisted image attachment in the database.
type ImageRecord struct {
	DiscordID    string
	MessageIndex int
	ImageID      string
	FilePath     string
	MIMEType     string
	Filename     string
}

// transientState holds per-user state that intentionally resets on restart.
type transientState struct {
	LastMessage  time.Time
	Scripted     bool
	WorldReady   bool
	Hatched      bool // true once hatching has started (prevents duplicate channels)
	MayorName    string
	WorldName    string
	WorldSummary string
	TemplateType string // "3d", "2d", "boardgame" — detected from conversation
	Creature     string // optional personality: "rogue AI", "tree spirit", etc.
	Vibe         string // optional personality: "snarky", "warm", "chaotic"
	Emoji        string // optional personality: single emoji
	CoverArtPath string // disk path to pending cover art (NOT image bytes)
	CoverArtMIME string
}

// ConversationManager manages per-user conversation state.
// Messages are persisted via a MessageStore; rate limits and scripted flags are in-memory.
type ConversationManager struct {
	store      MessageStore
	imageStore ImageStore // optional — nil if image uploads not supported
	mu         sync.RWMutex
	transient  map[string]*transientState // keyed by user ID
	rateLimit  time.Duration
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager(store MessageStore) *ConversationManager {
	cm := &ConversationManager{
		store:     store,
		transient: make(map[string]*transientState),
		rateLimit: 2 * time.Second,
	}
	go cm.cleanupLoop()
	return cm
}

// AddMessage adds a message to a user's conversation.
func (cm *ConversationManager) AddMessage(userID, role, content string) {
	_ = cm.store.AddMessage(userID, role, content)

	cm.mu.Lock()
	ts := cm.getOrCreate(userID)
	ts.LastMessage = time.Now()
	cm.mu.Unlock()
}

// GetMessages returns the conversation history for a user, with images merged in.
func (cm *ConversationManager) GetMessages(userID string) []Message {
	msgs, _ := cm.store.GetMessages(userID)
	if cm.imageStore == nil {
		return msgs
	}
	images, err := cm.imageStore.GetImages(userID)
	if err != nil || len(images) == 0 {
		return msgs
	}
	// Group images by message index.
	byIndex := make(map[int][]ImageAttachment)
	for _, img := range images {
		byIndex[img.MessageIndex] = append(byIndex[img.MessageIndex], ImageAttachment{
			ID:       img.ImageID,
			FilePath: img.FilePath,
			MIMEType: img.MIMEType,
			Filename: img.Filename,
		})
	}
	for i := range msgs {
		if attachments, ok := byIndex[i]; ok {
			msgs[i].Images = attachments
		}
	}
	return msgs
}

// SetImageStore sets the optional image store for persisting image attachments.
func (cm *ConversationManager) SetImageStore(store ImageStore) {
	cm.imageStore = store
}

// ImageStoreAvailable returns whether image uploads are supported.
func (cm *ConversationManager) ImageStoreAvailable() bool {
	return cm.imageStore != nil
}

// SetScripted marks a user's conversation as using the scripted fallback.
func (cm *ConversationManager) SetScripted(userID string, val bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.getOrCreate(userID).Scripted = val
}

// IsScripted returns whether the user's conversation is in scripted mode.
func (cm *ConversationManager) IsScripted(userID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ts, ok := cm.transient[userID]
	if !ok {
		return false
	}
	return ts.Scripted
}

// CheckRateLimit returns an error if the user is sending messages too fast.
func (cm *ConversationManager) CheckRateLimit(userID string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ts, ok := cm.transient[userID]
	if !ok {
		return nil
	}
	if time.Since(ts.LastMessage) < cm.rateLimit {
		return fmt.Errorf("please wait a moment before sending another message")
	}
	return nil
}

// SetWorldReady stores the world-ready info for later hatching.
func (cm *ConversationManager) SetWorldReady(userID string, info *WorldReadyInfo) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	ts := cm.getOrCreate(userID)
	ts.WorldReady = true
	ts.MayorName = info.MayorName
	ts.WorldName = info.WorldName
	ts.WorldSummary = info.WorldSummary
	ts.Creature = info.Creature
	ts.Vibe = info.Vibe
	ts.Emoji = info.Emoji
}

// GetWorldReady returns the stored world-ready info.
func (cm *ConversationManager) GetWorldReady(userID string) (info *WorldReadyInfo, ok bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ts, exists := cm.transient[userID]
	if !exists || !ts.WorldReady {
		return nil, false
	}
	return &WorldReadyInfo{
		MayorName:    ts.MayorName,
		WorldName:    ts.WorldName,
		WorldSummary: ts.WorldSummary,
		TemplateType: ts.TemplateType,
		Creature:     ts.Creature,
		Vibe:         ts.Vibe,
		Emoji:        ts.Emoji,
	}, true
}

// SetTemplateType stores the detected template type.
func (cm *ConversationManager) SetTemplateType(userID, templateType string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.getOrCreate(userID).TemplateType = templateType
}

// GetTemplateType returns the detected template type.
func (cm *ConversationManager) GetTemplateType(userID string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ts, ok := cm.transient[userID]
	if !ok {
		return ""
	}
	return ts.TemplateType
}

// SetCoverArtPath stores the disk path to the pending cover art.
func (cm *ConversationManager) SetCoverArtPath(userID, path, mimeType string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	ts := cm.getOrCreate(userID)
	ts.CoverArtPath = path
	ts.CoverArtMIME = mimeType
}

// GetCoverArtPath returns the disk path and MIME type of the pending cover art.
func (cm *ConversationManager) GetCoverArtPath(userID string) (path, mimeType string, ok bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ts, exists := cm.transient[userID]
	if !exists || ts.CoverArtPath == "" {
		return "", "", false
	}
	return ts.CoverArtPath, ts.CoverArtMIME, true
}

// AddImage persists an image attachment to the image store.
func (cm *ConversationManager) AddImage(userID string, messageIndex int, img ImageAttachment) error {
	if cm.imageStore == nil {
		return nil
	}
	return cm.imageStore.AddImage(userID, messageIndex, img.ID, img.FilePath, img.MIMEType, img.Filename)
}

// GetImageByID looks up an image by ID in the image store.
func (cm *ConversationManager) GetImageByID(imageID string) (filePath, mimeType string, ok bool) {
	if cm.imageStore != nil {
		rec, err := cm.imageStore.GetImageByID(imageID)
		if err == nil && rec != nil {
			return rec.FilePath, rec.MIMEType, true
		}
	}
	return "", "", false
}

// SetHatched atomically marks a user as hatching. Returns true if this call
// set the flag (first caller wins), false if already hatching/hatched.
func (cm *ConversationManager) SetHatched(userID string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	ts := cm.getOrCreate(userID)
	if ts.Hatched {
		return false
	}
	ts.Hatched = true
	return true
}

// ResetConversation clears both DB messages and in-memory transient state for a user.
func (cm *ConversationManager) ResetConversation(userID string) error {
	if err := cm.store.DeleteUserMessages(userID); err != nil {
		return err
	}
	// Delete persisted images and their files.
	if cm.imageStore != nil {
		if images, err := cm.imageStore.GetImages(userID); err == nil {
			for _, img := range images {
				_ = os.Remove(img.FilePath)
			}
		}
		_ = cm.imageStore.DeleteImages(userID)
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if ts, ok := cm.transient[userID]; ok {
		if ts.CoverArtPath != "" {
			_ = os.Remove(ts.CoverArtPath)
		}
		delete(cm.transient, userID)
	}
	return nil
}

// ClearWorldReady clears the world-ready state and removes any pending cover art file.
func (cm *ConversationManager) ClearWorldReady(userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	ts, ok := cm.transient[userID]
	if !ok {
		return
	}
	if ts.CoverArtPath != "" {
		_ = os.Remove(ts.CoverArtPath)
	}
	ts.WorldReady = false
	ts.Hatched = false
	ts.MayorName = ""
	ts.WorldName = ""
	ts.WorldSummary = ""
	ts.TemplateType = ""
	ts.Creature = ""
	ts.Vibe = ""
	ts.Emoji = ""
	ts.CoverArtPath = ""
	ts.CoverArtMIME = ""
}

// getOrCreate returns the transient state for a user, creating if needed.
// Must be called with cm.mu held for writing.
func (cm *ConversationManager) getOrCreate(userID string) *transientState {
	ts, ok := cm.transient[userID]
	if !ok {
		ts = &transientState{}
		cm.transient[userID] = ts
	}
	return ts
}

// cleanupLoop removes stale conversations and transient state.
func (cm *ConversationManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		_ = cm.store.DeleteOlderThan(24 * time.Hour)

		// Clean up old images.
		if cm.imageStore != nil {
			if paths, err := cm.imageStore.DeleteImagesOlderThan(24 * time.Hour); err == nil {
				for _, p := range paths {
					_ = os.Remove(p)
				}
			}
		}

		cm.mu.Lock()
		for id, ts := range cm.transient {
			if time.Since(ts.LastMessage) > 24*time.Hour {
				if ts.CoverArtPath != "" {
					_ = os.Remove(ts.CoverArtPath)
				}
				delete(cm.transient, id)
			}
		}
		cm.mu.Unlock()
	}
}
