package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
)

// imageGenSignals is used to read image generation signals from Datastar requests.
type imageGenSignals struct {
	ImagePrompt      string `json:"image_prompt"`       //nolint:tagliatelle // Datastar signal name
	ImageAspectRatio string `json:"image_aspect_ratio"` //nolint:tagliatelle // Datastar signal name
	ImageGenID       string `json:"image_gen_id"`       //nolint:tagliatelle // Datastar signal name
}

// handleImageGenerate generates an image from a text prompt via Gemini.
func (s *Server) handleImageGenerate(c echo.Context) error {
	if s.GeminiClient == nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		return sse.MarshalAndPatchSignals(map[string]any{
			"image_gen_status": "error",
			"image_error_msg":  "Image generation not configured (GEMINI_API_KEY not set)",
		})
	}

	var signals imageGenSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	prompt := strings.TrimSpace(signals.ImagePrompt)
	if prompt == "" {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		return sse.MarshalAndPatchSignals(map[string]any{
			"image_gen_status": "error",
			"image_error_msg":  "Please enter a description",
		})
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	if err := sse.MarshalAndPatchSignals(map[string]any{
		"image_gen_status":  "generating",
		"image_error_msg":   "",
		"image_preview_url": "",
		"image_gen_id":      "",
		"image_saved_path":  "",
	}); err != nil {
		return err
	}

	img, err := s.GeminiClient.Generate(
		c.Request().Context(),
		prompt,
		signals.ImageAspectRatio,
	)
	if err != nil {
		s.Logger.Error("image generation failed", "error", err, "prompt", prompt)

		return sse.MarshalAndPatchSignals(map[string]any{
			"image_gen_status": "error",
			"image_error_msg":  "Generation failed: " + err.Error(),
		})
	}

	return sse.MarshalAndPatchSignals(map[string]any{
		"image_gen_status":  "done",
		"image_gen_id":      img.ID,
		"image_preview_url": "/api/images/preview/" + img.ID,
		"image_prompt":      "",
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

		return sse.MarshalAndPatchSignals(map[string]any{
			"image_gen_status": "error",
			"image_error_msg":  "Image generation not configured",
		})
	}

	var signals imageGenSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	img := s.GeminiClient.GetCached(signals.ImageGenID)
	if img == nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		return sse.MarshalAndPatchSignals(map[string]any{
			"image_gen_status": "error",
			"image_error_msg":  "Image expired or not found — generate again",
		})
	}

	// Generate filename from timestamp + prompt slug.
	slug := slugify(img.Prompt)
	ext := extensionForMIME(img.MIMEType)
	filename := fmt.Sprintf("%s-%s%s", time.Now().Format("20060102-150405"), slug, ext)

	genDir := filepath.Join(s.DataDir, "shared-assets", "generated")
	if err := os.MkdirAll(genDir, 0o750); err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create directory",
		)
	}

	destPath := filepath.Join(genDir, filename)
	if err := os.WriteFile(destPath, img.Data, 0o600); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to write file")
	}

	s.GeminiClient.RemoveCached(signals.ImageGenID)

	savedPath := "generated/" + filename
	s.Logger.Info("saved generated image", "path", savedPath, "size", len(img.Data))

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	return sse.MarshalAndPatchSignals(map[string]any{
		"image_gen_status": "saved",
		"image_saved_path": savedPath,
	})
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
