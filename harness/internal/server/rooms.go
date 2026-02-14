package server

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png" // register PNG decoder
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/views/imagegen"
)

// Placement default values for signal reset.
const (
	defaultPlaceScale = 0.2
	defaultPlaceX     = 640
	defaultPlaceY     = 360
)

// roomJSON mirrors the 2D template's room JSON schema for round-tripping edits.
type roomJSON struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	BackgroundColor string        `json:"background_color"`           //nolint:tagliatelle // matches room JSON schema
	BackgroundImage *string       `json:"background_image,omitempty"` //nolint:tagliatelle // matches room JSON schema
	Hotspots        []hotspotJSON `json:"hotspots"`
}

// hotspotJSON mirrors a single hotspot entry in a room JSON file.
type hotspotJSON struct {
	ID     string          `json:"id"`
	Label  string          `json:"label"`
	X      float64         `json:"x"`
	Y      float64         `json:"y"`
	Width  float64         `json:"width"`
	Height float64         `json:"height"`
	Image  *string         `json:"image,omitempty"`
	Action json.RawMessage `json:"action"`
}

// placeSignals is used to read placement-related signals from requests.
type placeSignals struct {
	PlaceAssetPath string  `json:"place_asset_path"` //nolint:tagliatelle // Datastar signal name
	PlaceScale     float64 `json:"place_scale"`      //nolint:tagliatelle // Datastar signal name
	PlaceX         float64 `json:"place_x"`          //nolint:tagliatelle // Datastar signal name
	PlaceY         float64 `json:"place_y"`          //nolint:tagliatelle // Datastar signal name
	PlaceLabel     string  `json:"place_label"`      //nolint:tagliatelle // Datastar signal name
}

// handleRoomList returns the list of available rooms as an SSE fragment.
func (s *Server) handleRoomList(c echo.Context) error {
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	roomsDir := filepath.Join(s.DataDir, "shared-assets", "rooms")

	rooms, err := listRooms(roomsDir)
	if err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Failed to list rooms: " + err.Error()),
		)
	}

	return sse.PatchElementTempl(imagegen.PlacementRoomList(rooms))
}

// handleRoomHotspots returns the hotspot list for a room plus placement options.
func (s *Server) handleRoomHotspots(c echo.Context) error {
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	roomID := c.Param("roomID")
	roomsDir := filepath.Join(s.DataDir, "shared-assets", "rooms")

	room, err := readRoomJSON(filepath.Join(roomsDir, roomID+".room.json"))
	if err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Room not found: " + err.Error()),
		)
	}

	hotspots := make([]imagegen.HotspotInfo, len(room.Hotspots))
	for i, h := range room.Hotspots {
		hotspots[i] = imagegen.HotspotInfo{
			ID:       h.ID,
			Label:    h.Label,
			HasImage: h.Image != nil,
			Width:    h.Width,
			Height:   h.Height,
		}
	}

	return sse.PatchElementTempl(imagegen.PlacementTargets(
		imagegen.RoomInfo{ID: room.ID, Name: room.Name},
		hotspots,
		room.BackgroundImage != nil,
	))
}

// handlePlaceBackground sets a room's background_image to the placed asset.
func (s *Server) handlePlaceBackground(c echo.Context) error {
	var signals placeSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	roomID := c.Param("roomID")
	roomPath := filepath.Join(s.DataDir, "shared-assets", "rooms", roomID+".room.json")

	room, err := readRoomJSON(roomPath)
	if err != nil {
		return sse.PatchElementTempl(imagegen.PlacementError("Room not found"))
	}

	assetPath := strings.TrimSpace(signals.PlaceAssetPath)
	if assetPath == "" {
		return sse.PatchElementTempl(imagegen.PlacementError("No asset selected"))
	}

	room.BackgroundImage = &assetPath

	if err := writeRoomJSON(roomPath, room); err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Failed to save: " + err.Error()),
		)
	}

	s.Logger.Info("placed background image", "room", roomID, "asset", assetPath)

	return sse.PatchElementTempl(imagegen.PlacementSuccess(room.Name, "background"))
}

// handlePlaceOnHotspot sets the image on an existing hotspot.
func (s *Server) handlePlaceOnHotspot(c echo.Context) error {
	var signals placeSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	roomID := c.Param("roomID")
	hotspotID := c.Param("hotspotID")
	roomPath := filepath.Join(
		s.DataDir, "shared-assets", "rooms", roomID+".room.json",
	)

	room, err := readRoomJSON(roomPath)
	if err != nil {
		return sse.PatchElementTempl(imagegen.PlacementError("Room not found"))
	}

	assetPath := strings.TrimSpace(signals.PlaceAssetPath)
	if assetPath == "" {
		return sse.PatchElementTempl(imagegen.PlacementError("No asset selected"))
	}

	// Read image dimensions to set hotspot size preserving aspect ratio.
	imgDim, err := getImageDimensions(
		filepath.Join(s.DataDir, "shared-assets", assetPath),
	)
	if err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Cannot read image: " + err.Error()),
		)
	}

	// Find the hotspot and update it.
	found := false

	for i := range room.Hotspots {
		if room.Hotspots[i].ID != hotspotID {
			continue
		}

		room.Hotspots[i].Image = &assetPath
		resizeHotspotToImage(&room.Hotspots[i], imgDim)
		found = true

		break
	}

	if !found {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Hotspot not found: " + hotspotID),
		)
	}

	if err := writeRoomJSON(roomPath, room); err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Failed to save: " + err.Error()),
		)
	}

	s.Logger.Info(
		"placed image on hotspot",
		"room", roomID, "hotspot", hotspotID, "asset", assetPath,
	)

	return sse.PatchElementTempl(
		imagegen.PlacementSuccess(room.Name, "hotspot \""+hotspotID+"\""),
	)
}

// resizeHotspotToImage adjusts a hotspot's width/height to match the image's
// aspect ratio while keeping the center position and using the larger existing
// dimension as the constraint.
func resizeHotspotToImage(h *hotspotJSON, dim imagegen.ImageDimensions) {
	centerX := h.X + h.Width/2  //nolint:mnd // center = origin + half
	centerY := h.Y + h.Height/2 //nolint:mnd // center = origin + half
	aspectRatio := float64(dim.Width) / float64(dim.Height)

	maxDim := h.Width
	if h.Height > maxDim {
		maxDim = h.Height
	}

	if aspectRatio >= 1 {
		h.Width = maxDim
		h.Height = maxDim / aspectRatio
	} else {
		h.Height = maxDim
		h.Width = maxDim * aspectRatio
	}

	h.X = centerX - h.Width/2  //nolint:mnd // re-center
	h.Y = centerY - h.Height/2 //nolint:mnd // re-center
}

// handlePlaceNewHotspot creates a new hotspot with the placed image.
func (s *Server) handlePlaceNewHotspot(c echo.Context) error {
	var signals placeSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	roomID := c.Param("roomID")
	roomPath := filepath.Join(
		s.DataDir, "shared-assets", "rooms", roomID+".room.json",
	)

	room, err := readRoomJSON(roomPath)
	if err != nil {
		return sse.PatchElementTempl(imagegen.PlacementError("Room not found"))
	}

	assetPath := strings.TrimSpace(signals.PlaceAssetPath)
	if assetPath == "" {
		return sse.PatchElementTempl(imagegen.PlacementError("No asset selected"))
	}

	// Read image dimensions.
	imgDim, err := getImageDimensions(
		filepath.Join(s.DataDir, "shared-assets", assetPath),
	)
	if err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Cannot read image: " + err.Error()),
		)
	}

	// Compute scaled dimensions preserving aspect ratio.
	scale := signals.PlaceScale
	if scale <= 0 || scale > 1 {
		scale = defaultPlaceScale
	}

	width := float64(imgDim.Width) * scale
	height := float64(imgDim.Height) * scale

	// Position: center the image on the specified point (top-left coords in 1280x720 space).
	x := signals.PlaceX - width/2  //nolint:mnd // center offset
	y := signals.PlaceY - height/2 //nolint:mnd // center offset

	label := strings.TrimSpace(signals.PlaceLabel)
	if label == "" {
		label = strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	}

	hsID := slugify(label)

	// Ensure uniqueness.
	for _, h := range room.Hotspots {
		if h.ID == hsID {
			hsID += "-2"

			break
		}
	}

	action, err := json.Marshal(map[string]string{
		"type": "dialog",
		"text": label,
	})
	if err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Failed to create action: " + err.Error()),
		)
	}

	room.Hotspots = append(room.Hotspots, hotspotJSON{
		ID:     hsID,
		Label:  label,
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
		Image:  &assetPath,
		Action: action,
	})

	if err := writeRoomJSON(roomPath, room); err != nil {
		return sse.PatchElementTempl(
			imagegen.PlacementError("Failed to save: " + err.Error()),
		)
	}

	s.Logger.Info(
		"placed new hotspot",
		"room",
		roomID,
		"hotspot",
		hsID,
		"asset",
		assetPath,
	)

	if err := sse.PatchElementTempl(
		imagegen.PlacementSuccess(room.Name, "new hotspot \""+hsID+"\""),
	); err != nil {
		return err
	}

	return sse.MarshalAndPatchSignals(map[string]any{
		"place_asset_path": "",
		"place_label":      "",
		"place_scale":      defaultPlaceScale,
		"place_x":          defaultPlaceX,
		"place_y":          defaultPlaceY,
	})
}

// listRooms reads all .room.json files and returns basic info.
func listRooms(roomsDir string) ([]imagegen.RoomInfo, error) {
	entries, err := os.ReadDir(roomsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []imagegen.RoomInfo{}, nil
		}

		return nil, err
	}

	var rooms []imagegen.RoomInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".room.json") {
			continue
		}

		room, readErr := readRoomJSON(filepath.Join(roomsDir, entry.Name()))
		if readErr != nil {
			continue
		}

		rooms = append(rooms, imagegen.RoomInfo{
			ID:   room.ID,
			Name: room.Name,
		})
	}

	return rooms, nil
}

// readRoomJSON reads and parses a room JSON file.
func readRoomJSON(path string) (*roomJSON, error) {
	cleanPath := filepath.Clean(path)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	var room roomJSON
	if err := json.Unmarshal(data, &room); err != nil {
		return nil, fmt.Errorf("invalid room JSON: %w", err)
	}

	return &room, nil
}

// writeRoomJSON writes a room struct back to JSON with indentation.
func writeRoomJSON(path string, room *roomJSON) error {
	data, err := json.MarshalIndent(room, "", "    ")
	if err != nil {
		return err
	}

	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

// getImageDimensions reads just the header of an image to get its pixel dimensions.
func getImageDimensions(path string) (imagegen.ImageDimensions, error) {
	cleanPath := filepath.Clean(path)

	f, err := os.Open(cleanPath)
	if err != nil {
		return imagegen.ImageDimensions{}, err
	}

	defer func() { _ = f.Close() }()

	config, _, err := image.DecodeConfig(f)
	if err != nil {
		return imagegen.ImageDimensions{}, err
	}

	return imagegen.ImageDimensions{
		Width:  config.Width,
		Height: config.Height,
	}, nil
}
