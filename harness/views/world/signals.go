package world

// Default placement signal values.
const (
	defaultPlaceScale = 0.2
	defaultPlaceX     = 640
	defaultPlaceY     = 360
)

// OverlaySignals defines all reactive signals for the game overlay.
// These are initialized on the harness-overlay element via data-signals
// and updated by the SSE handler via MarshalAndPatchSignals.
type OverlaySignals struct {
	CurrentWorldID      string  `json:"current_world_id"`      //nolint:tagliatelle // Datastar signal names use snake_case
	CurrentCheckpointID string  `json:"current_checkpoint_id"` //nolint:tagliatelle // Datastar signal names use snake_case
	BuildStatus         string  `json:"build_status"`          //nolint:tagliatelle // Datastar signal names use snake_case
	PromptText          string  `json:"prompt_text"`           //nolint:tagliatelle // Datastar signal names use snake_case
	ChatText            string  `json:"chat_text"`             //nolint:tagliatelle // Datastar signal names use snake_case
	OverlayExpanded     bool    `json:"overlay_expanded"`      //nolint:tagliatelle // Datastar signal names use snake_case
	ActiveTab           string  `json:"active_tab"`            //nolint:tagliatelle // Datastar signal names use snake_case
	ShowCheckpointTree  bool    `json:"show_checkpoint_tree"`  //nolint:tagliatelle // Datastar signal names use snake_case
	UnreadCount         int     `json:"unread_count"`          //nolint:tagliatelle // Datastar signal names use snake_case
	RateLimitRetryAt    int64   `json:"rate_limit_retry_at"`   //nolint:tagliatelle // Datastar signal names use snake_case
	ImagePrompt         string  `json:"image_prompt"`          //nolint:tagliatelle // Datastar signal names use snake_case
	ImageAspectRatio    string  `json:"image_aspect_ratio"`    //nolint:tagliatelle // Datastar signal names use snake_case
	ImageTransparentBG  bool    `json:"image_transparent_bg"`  //nolint:tagliatelle // Datastar signal names use snake_case
	AssetsGenOpen       bool    `json:"assets_gen_open"`       //nolint:tagliatelle // Datastar signal names use snake_case
	PlaceAssetPath      string  `json:"place_asset_path"`      //nolint:tagliatelle // Datastar signal names use snake_case
	PlaceScale          float64 `json:"place_scale"`           //nolint:tagliatelle // Datastar signal names use snake_case
	PlaceX              float64 `json:"place_x"`               //nolint:tagliatelle // Datastar signal names use snake_case
	PlaceY              float64 `json:"place_y"`               //nolint:tagliatelle // Datastar signal names use snake_case
	PlaceLabel          string  `json:"place_label"`           //nolint:tagliatelle // Datastar signal names use snake_case
}

// DefaultOverlaySignals returns the default signal state for a world overlay.
func DefaultOverlaySignals(worldID, cpID string) OverlaySignals {
	return OverlaySignals{
		CurrentWorldID:      worldID,
		CurrentCheckpointID: cpID,
		BuildStatus:         "idle",
		OverlayExpanded:     false,
		ActiveTab:           "global",
		ImageAspectRatio:    "1:1",
		ImageTransparentBG:  true,
		AssetsGenOpen:       true,
		PlaceScale:          defaultPlaceScale,
		PlaceX:              defaultPlaceX,
		PlaceY:              defaultPlaceY,
	}
}
