package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/views/imagegen"
)

// imageGenSignals is used to read signals for the generate action.
type imageGenSignals struct {
	ImagePrompt        string `json:"image_prompt"`         //nolint:tagliatelle // Datastar signal name
	ImageAspectRatio   string `json:"image_aspect_ratio"`   //nolint:tagliatelle // Datastar signal name
	ImageTransparentBG bool   `json:"image_transparent_bg"` //nolint:tagliatelle // Datastar signal name
}

// imageSaveSignals is used to read signals for the save action.
type imageSaveSignals struct {
	ImageGenID string `json:"image_gen_id"` //nolint:tagliatelle // Datastar signal name
}

// handleImageGenerate generates an image from a text prompt via Gemini.
func (s *Server) handleImageGenerate(c echo.Context) error {
	if s.GeminiClient == nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		return sse.PatchElementTempl(
			imagegen.ImageGenError(
				"Image generation not configured (GEMINI_API_KEY not set)",
			),
		)
	}

	// ReadSignals MUST be called before NewSSE — NewSSE flushes response
	// headers which can invalidate the request body.
	var signals imageGenSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	prompt := strings.TrimSpace(signals.ImagePrompt)

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	if prompt == "" {
		return sse.PatchElementTempl(
			imagegen.ImageGenError("Please enter a description"),
		)
	}

	// Show spinner immediately.
	if err := sse.PatchElementTempl(imagegen.ImageGenGenerating()); err != nil {
		return err
	}

	img, err := s.GeminiClient.Generate(
		c.Request().Context(),
		prompt,
		signals.ImageAspectRatio,
		signals.ImageTransparentBG,
	)
	if err != nil {
		s.Logger.Error("image generation failed", "error", err, "prompt", prompt)

		return sse.PatchElementTempl(
			imagegen.ImageGenError("Generation failed: " + err.Error()),
		)
	}

	// Patch in the preview fragment and clear the prompt input.
	if err := sse.PatchElementTempl(
		imagegen.ImageGenDone(img.ID, "/api/images/preview/"+img.ID),
	); err != nil {
		return err
	}

	return sse.MarshalAndPatchSignals(map[string]any{
		"image_prompt": "",
	})
}

// handleImagePreview serves a cached generated image by ID.
func (s *Server) handleImagePreview(c echo.Context) error {
	if s.GeminiClient == nil {
		return echo.NewHTTPError(http.StatusNotFound, "image generation not configured")
	}

	genID := c.Param("genID")
	img := s.GeminiClient.GetCached(genID)

	if img == nil {
		return echo.NewHTTPError(http.StatusNotFound, "image not found or expired")
	}

	mimeType := img.MIMEType
	if mimeType == "" {
		mimeType = "image/png"
	}

	return c.Blob(http.StatusOK, mimeType, img.Data)
}

// handleImageSave writes a cached generated image to shared-assets/generated/.
func (s *Server) handleImageSave(c echo.Context) error {
	if s.GeminiClient == nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		return sse.PatchElementTempl(
			imagegen.ImageGenError("Image generation not configured"),
		)
	}

	// ReadSignals MUST be called before NewSSE.
	var signals imageSaveSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	img := s.GeminiClient.GetCached(signals.ImageGenID)
	if img == nil {
		return sse.PatchElementTempl(
			imagegen.ImageGenError("Image expired or not found — generate again"),
		)
	}

	// Generate filename from timestamp + prompt slug.
	slug := slugify(img.Prompt)
	ext := extensionForMIME(img.MIMEType)
	filename := fmt.Sprintf(
		"%s-%s%s",
		time.Now().Format("20060102-150405"),
		slug,
		ext,
	)

	genDir := filepath.Join(s.DataDir, "shared-assets", "generated")
	if err := os.MkdirAll(genDir, 0o750); err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create directory",
		)
	}

	destPath := filepath.Join(genDir, filename)
	if err := os.WriteFile(destPath, img.Data, 0o600); err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to write file",
		)
	}

	s.GeminiClient.RemoveCached(signals.ImageGenID)

	savedPath := "generated/" + filename
	s.Logger.Info("saved generated image", "path", savedPath, "size", len(img.Data))

	if err := sse.PatchElementTempl(imagegen.ImageGenSaved(savedPath)); err != nil {
		return err
	}

	files, _ := listGeneratedAssets(genDir)

	return sse.PatchElementTempl(imagegen.AssetTree(files))
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a prompt string to a URL-safe slug (max 40 chars).
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' {
			return r
		}

		return ' '
	}, s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	const maxSlugLen = 40
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		s = strings.TrimRight(s, "-")
	}

	if s == "" {
		s = "image"
	}

	return s
}

// handleAssetTree serves the generated assets file tree via SSE.
func (s *Server) handleAssetTree(c echo.Context) error {
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	genDir := filepath.Join(s.DataDir, "shared-assets", "generated")
	files, _ := listGeneratedAssets(genDir)

	return sse.PatchElementTempl(imagegen.AssetTree(files))
}

// listGeneratedAssets reads the generated directory and returns file metadata
// sorted by modification time descending (newest first).
func listGeneratedAssets(genDir string) ([]imagegen.AssetFileInfo, error) {
	entries, err := os.ReadDir(genDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []imagegen.AssetFileInfo{}, nil
		}

		return nil, err
	}

	type fileWithTime struct {
		info imagegen.AssetFileInfo
		mod  time.Time
	}

	var items []fileWithTime

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, fileWithTime{
			info: imagegen.AssetFileInfo{
				Filename: entry.Name(),
				Path:     "generated/" + entry.Name(),
				SizeKB:   info.Size() / 1024, //nolint:mnd // bytes to KB
				ModTime:  info.ModTime().Format("Jan 2, 3:04 PM"),
			},
			mod: info.ModTime(),
		})
	}

	// Sort newest first.
	sort.Slice(items, func(i, j int) bool {
		return items[i].mod.After(items[j].mod)
	})

	files := make([]imagegen.AssetFileInfo, len(items))
	for i, item := range items {
		files[i] = item.info
	}

	return files, nil
}

// extensionForMIME returns a file extension for the given MIME type.
func extensionForMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
