package imagegen

// AssetFileInfo holds metadata about an asset file for display in the tree.
type AssetFileInfo struct {
	Filename string
	Path     string // relative path under /assets/ (e.g. "generated/20260213-cat.png")
	SizeKB   int64
	ModTime  string // formatted like "Feb 13, 2:45 PM"
}

// AssetFolder groups files under a folder name for the asset tree.
type AssetFolder struct {
	Name  string          // folder name (e.g. "generated", "rooms")
	Files []AssetFileInfo // files in the folder, newest first
}

// RoomInfo holds basic room metadata for the placement room list.
type RoomInfo struct {
	ID   string
	Name string
}

// HotspotInfo holds hotspot metadata for the placement target list.
type HotspotInfo struct {
	ID       string
	Label    string
	HasImage bool
	Width    float64
	Height   float64
}

// ImageDimensions holds the native pixel dimensions of an image.
type ImageDimensions struct {
	Width  int
	Height int
}
