package mayor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/coreycole/creative-mode/site/internal/auth"
)

const (
	maxImageSize  = 5 << 20 // 5 MB
	maxImageCount = 4
)

// HandleImageUpload processes a multipart image upload.
// POST /mayor/upload
func (h *Handler) HandleImageUpload(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	// Check image count cap.
	pending := h.convMgr.GetPendingImages(session.DiscordID)
	if len(pending) >= maxImageCount {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Maximum %d images allowed per message", maxImageCount),
		})
	}

	// Read multipart file.
	file, header, err := c.Request().FormFile("image")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "no image file provided")
	}
	defer func() { _ = file.Close() }()

	// Check file size.
	if header.Size > maxImageSize {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Image must be under 5 MB",
		})
	}

	// Read file data and sniff MIME type.
	data, err := io.ReadAll(io.LimitReader(file, maxImageSize+1))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read file")
	}
	if len(data) > maxImageSize {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Image must be under 5 MB",
		})
	}

	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		// OK
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Unsupported image type. Use JPEG, PNG, GIF, or WebP.",
		})
	}

	// Save to disk.
	imageID := uuid.New().String()
	ext := mayorchat.MimeToExt(mimeType)
	uploadDir := filepath.Join(h.dataDir, "chat-uploads", session.DiscordID)
	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create upload directory")
	}
	filePath := filepath.Join(uploadDir, imageID+ext)
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save image")
	}

	// Add to pending images.
	h.convMgr.AddPendingImage(session.DiscordID, mayorchat.PendingImage{
		ID:       imageID,
		FilePath: filePath,
		MIMEType: mimeType,
		Filename: header.Filename,
	})

	return c.JSON(http.StatusOK, map[string]string{
		"id":       imageID,
		"url":      "/mayor/image/" + imageID,
		"filename": header.Filename,
	})
}

// HandleImageServe serves an uploaded image by its ID.
// GET /mayor/image/:imageID
func (h *Handler) HandleImageServe(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	imageID := c.Param("imageID")
	filePath, _, ok := h.convMgr.GetImageByID(session.DiscordID, imageID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "image not found")
	}

	return c.File(filePath)
}

// HandleImageDelete removes a pending image.
// DELETE /mayor/image/:imageID
func (h *Handler) HandleImageDelete(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	imageID := c.Param("imageID")
	filePath, ok := h.convMgr.RemovePendingImage(session.DiscordID, imageID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "image not found")
	}

	_ = os.Remove(filePath)
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
