package lobby

// LobbySignals defines the reactive signals for the lobby page.
// Used by the chat input binding and SSE connection.
type LobbySignals struct {
	ChatText string `json:"chat_text"` //nolint:tagliatelle // Datastar signal name
}

// DefaultLobbySignals returns the default signal state for the lobby.
func DefaultLobbySignals() LobbySignals {
	return LobbySignals{}
}
