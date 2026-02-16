package gemini

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/coreycole/creative-mode/pkg/imagegen"
	"github.com/google/uuid"
)

const (
	cacheTTL      = 30 * time.Minute
	cacheMaxItems = 50

	// chromakeySuffix is appended to every prompt to get a removable background.
	chromakeySuffix = " on a solid, flat, uniform chromakey green background. " +
		"Use EXACTLY hex color #00FF00 (RGB 0, 255, 0). " +
		"The entire background must be this single pure green color with " +
		"NO variation, NO gradients, NO shadows, NO lighting effects on the background."

	// HSV thresholds for green detection.
	greenHueCenter = 120.0 // pure green in degrees
	greenHueRange  = 30.0  // +/- degrees around center
	greenSatMin    = 0.50  // minimum saturation (0-1)
	greenValMin    = 0.30  // minimum value/brightness (0-1)

	// dilateRadius controls edge cleanup (pixels around detected green).
	dilateRadius = 1

	// Color conversion constants.
	colorShift8   = 8     // bit shift for 16-bit to 8-bit color conversion
	colorMax      = 255.0 // max 8-bit color value
	hueFullCircle = 360.0 // degrees in a full hue circle
	hueHalf       = 180.0 // half circle for wrap-around distance
	hueSextant    = 60.0  // degrees per HSV sextant
	hueSextants   = 6     // number of sextants in hue circle
	hueOffsetG    = 2.0   // green sextant offset
	hueOffsetB    = 4.0   // blue sextant offset
)

// GeneratedImage holds a generated image in memory until saved or expired.
type GeneratedImage struct {
	ID        string
	Data      []byte
	MIMEType  string
	Prompt    string
	CreatedAt time.Time
}

// Client wraps the shared imagegen.Client with caching and chromakey support.
type Client struct {
	core   *imagegen.Client
	logger *slog.Logger
	mu     sync.RWMutex
	cache  map[string]*GeneratedImage
}

// NewClient creates a Gemini client. Returns nil, nil if apiKey is empty (feature disabled).
func NewClient(ctx context.Context, apiKey string, logger *slog.Logger) (*Client, error) {
	core, err := imagegen.NewClient(ctx, apiKey, logger)
	if err != nil {
		return nil, err
	}
	if core == nil {
		return nil, nil //nolint:nilnil // nil client means feature disabled
	}

	return &Client{
		core:   core,
		logger: logger,
		cache:  make(map[string]*GeneratedImage),
	}, nil
}

// Generate calls Gemini to generate an image from a text prompt.
// When transparentBG is true, a chromakey green background is requested and
// then removed to produce a PNG with alpha transparency. When false, the raw
// image bytes are returned as-is with their detected MIME type.
func (c *Client) Generate(
	ctx context.Context,
	prompt, aspectRatio string,
	transparentBG bool,
) (*GeneratedImage, error) {
	c.evictExpired()

	opts := imagegen.GenerateOptions{
		AspectRatio: aspectRatio,
	}
	if transparentBG {
		opts.PromptSuffix = chromakeySuffix
	}

	raw, err := c.core.Generate(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}

	imgData := raw.Data
	mimeType := raw.MIMEType

	if transparentBG {
		pngData, chromaErr := removeGreenBackground(raw.Data)
		if chromaErr != nil {
			c.logger.Warn("chromakey removal failed, using raw image",
				"error", chromaErr)
		} else {
			imgData = pngData
			mimeType = "image/png"
		}
	}

	img := &GeneratedImage{
		ID:        uuid.New().String()[:8],
		Data:      imgData,
		MIMEType:  mimeType,
		Prompt:    prompt,
		CreatedAt: time.Now(),
	}

	c.mu.Lock()
	c.cache[img.ID] = img
	c.mu.Unlock()

	c.logger.Info("generated image",
		"id", img.ID,
		"size", len(imgData),
		"mimeType", mimeType,
		"transparentBG", transparentBG,
		"prompt", prompt,
	)

	return img, nil
}

// GetCached returns a cached generated image by ID, or nil if not found/expired.
func (c *Client) GetCached(id string) *GeneratedImage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	img, ok := c.cache[id]
	if !ok {
		return nil
	}

	if time.Since(img.CreatedAt) > cacheTTL {
		return nil
	}

	return img
}

// RemoveCached removes a generated image from the cache (after save).
func (c *Client) RemoveCached(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, id)
}

// removeGreenBackground decodes an image (JPEG or PNG), detects chromakey green
// pixels in HSV color space, sets their alpha to 0, and re-encodes as PNG.
func removeGreenBackground(data []byte) ([]byte, error) {
	// Gemini sometimes returns JPEG bytes despite claiming PNG MIME type.
	// Try both decoders.
	src, err := decodeImage(data)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	// Build a mask of green pixels.
	w, h := bounds.Dx(), bounds.Dy()
	greenMask := make([]bool, w*h)

	for y := range h {
		for x := range w {
			r, g, b, _ := dst.NRGBAAt(x, y).RGBA()
			// Convert from 0-65535 to 0-255.
			rf := float64(r >> colorShift8)
			gf := float64(g >> colorShift8)
			bf := float64(b >> colorShift8)

			if isChromakeyGreen(rf, gf, bf) {
				greenMask[y*w+x] = true
			}
		}
	}

	// Dilate the mask to catch anti-aliased edge pixels.
	dilated := dilateMask(greenMask, w, h, dilateRadius)

	// Apply mask: set alpha to 0 for green pixels.
	for y := range h {
		for x := range w {
			if dilated[y*w+x] {
				dst.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}

	return buf.Bytes(), nil
}

// decodeImage tries PNG first, then JPEG (Gemini sometimes returns JPEG
// bytes despite reporting image/png MIME type).
func decodeImage(data []byte) (image.Image, error) {
	// Check for JPEG magic bytes (0xFF 0xD8).
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		return jpeg.Decode(bytes.NewReader(data))
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		// Fallback to JPEG if PNG fails.
		return jpeg.Decode(bytes.NewReader(data))
	}

	return img, nil
}

// isChromakeyGreen checks if an RGB color (0-255 range) is chromakey green in HSV space.
func isChromakeyGreen(r, g, b float64) bool {
	h, s, v := rgbToHSV(r/colorMax, g/colorMax, b/colorMax)

	hueDist := math.Abs(h - greenHueCenter)
	if hueDist > hueHalf {
		hueDist = hueFullCircle - hueDist
	}

	return hueDist <= greenHueRange && s >= greenSatMin && v >= greenValMin
}

// rgbToHSV converts RGB (0-1 range) to HSV (H: 0-360, S: 0-1, V: 0-1).
func rgbToHSV(r, g, b float64) (h, s, v float64) {
	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	delta := maxC - minC

	v = maxC

	if maxC == 0 {
		return 0, 0, v
	}

	s = delta / maxC

	if delta == 0 {
		return 0, s, v
	}

	switch maxC {
	case r:
		h = hueSextant * math.Mod((g-b)/delta, hueSextants)
	case g:
		h = hueSextant * ((b-r)/delta + hueOffsetG)
	case b:
		h = hueSextant * ((r-g)/delta + hueOffsetB)
	}

	if h < 0 {
		h += hueFullCircle
	}

	return h, s, v
}

// dilateMask expands true pixels in the mask by the given radius.
func dilateMask(mask []bool, w, h, radius int) []bool {
	if radius <= 0 {
		return mask
	}

	result := make([]bool, len(mask))
	copy(result, mask)

	for range radius {
		next := make([]bool, len(result))
		copy(next, result)

		for y := range h {
			for x := range w {
				if !result[y*w+x] {
					continue
				}
				// Expand to 4-connected neighbors.
				for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					nx, ny := x+d[0], y+d[1]
					if nx >= 0 && nx < w && ny >= 0 && ny < h {
						next[ny*w+nx] = true
					}
				}
			}
		}

		result = next
	}

	return result
}

// evictExpired removes expired entries and enforces max cache size.
func (c *Client) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for id, img := range c.cache {
		if now.Sub(img.CreatedAt) > cacheTTL {
			delete(c.cache, id)
		}
	}

	// If still over limit, remove oldest.
	for len(c.cache) >= cacheMaxItems {
		var oldestID string
		var oldestTime time.Time

		for id, img := range c.cache {
			if oldestID == "" || img.CreatedAt.Before(oldestTime) {
				oldestID = id
				oldestTime = img.CreatedAt
			}
		}

		if oldestID != "" {
			delete(c.cache, oldestID)
		}
	}
}
