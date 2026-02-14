package auth

import (
	"strings"
	"sync"
)

// InviteCodeManager validates invite codes from a CSV string.
type InviteCodeManager struct {
	mu    sync.RWMutex
	codes map[string]bool
}

// NewInviteCodeManager creates a new invite code manager from a comma-separated string.
func NewInviteCodeManager(codesCSV string) *InviteCodeManager {
	codes := make(map[string]bool)
	for _, code := range strings.Split(codesCSV, ",") {
		code = strings.TrimSpace(code)
		if code != "" {
			codes[strings.ToLower(code)] = true
		}
	}
	return &InviteCodeManager{codes: codes}
}

// VerifyCode checks if an invite code is valid. Case-insensitive.
func (m *InviteCodeManager) VerifyCode(code string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.codes[strings.ToLower(strings.TrimSpace(code))]
}
