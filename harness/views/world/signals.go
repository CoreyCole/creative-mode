package world

// OverlaySignals defines all reactive signals for the game overlay.
// These are initialized on the harness-overlay element via data-signals
// and updated by the SSE handler via MarshalAndPatchSignals.
type OverlaySignals struct {
	CurrentWorldID      string `json:"current_world_id"`      //nolint:tagliatelle // Datastar signal names use snake_case
	CurrentCheckpointID string `json:"current_checkpoint_id"` //nolint:tagliatelle // Datastar signal names use snake_case
	BuildStatus         string `json:"build_status"`          //nolint:tagliatelle // Datastar signal names use snake_case
	PromptText          string `json:"prompt_text"`           //nolint:tagliatelle // Datastar signal names use snake_case
	ChatText            string `json:"chat_text"`             //nolint:tagliatelle // Datastar signal names use snake_case
	OverlayExpanded     bool   `json:"overlay_expanded"`      //nolint:tagliatelle // Datastar signal names use snake_case
	ActiveTab           string `json:"active_tab"`            //nolint:tagliatelle // Datastar signal names use snake_case
	ShowCheckpointTree  bool   `json:"show_checkpoint_tree"`  //nolint:tagliatelle // Datastar signal names use snake_case
	UnreadCount         int    `json:"unread_count"`          //nolint:tagliatelle // Datastar signal names use snake_case
	RateLimitRetryAt    int64  `json:"rate_limit_retry_at"`   //nolint:tagliatelle // Datastar signal names use snake_case
	ImagePrompt         string `json:"image_prompt"`          //nolint:tagliatelle // Datastar signal names use snake_case
	ImageGenStatus      string `json:"image_gen_status"`      //nolint:tagliatelle // Datastar signal names use snake_case
	ImageGenID          string `json:"image_gen_id"`          //nolint:tagliatelle // Datastar signal names use snake_case
	ImagePreviewURL     string `json:"image_preview_url"`     //nolint:tagliatelle // Datastar signal names use snake_case
	ImageSavedPath      string `json:"image_saved_path"`      //nolint:tagliatelle // Datastar signal names use snake_case
	ImageErrorMsg       string `json:"image_error_msg"`       //nolint:tagliatelle // Datastar signal names use snake_case
	ImageAspectRatio    string `json:"image_aspect_ratio"`    //nolint:tagliatelle // Datastar signal names use snake_case
}

// DefaultOverlaySignals returns the default signal state for a world overlay.
func DefaultOverlaySignals(worldID, cpID string) OverlaySignals {
	return OverlaySignals{
		CurrentWorldID:      worldID,
		CurrentCheckpointID: cpID,
		BuildStatus:         "idle",
		OverlayExpanded:     false,
		ActiveTab:           "global",
		ImageGenStatus:      "idle",
		ImageAspectRatio:    "1:1",
	}
}
