package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// allowedImageMIME is the set of MIME types accepted by the asset upload handler.
var allowedImageMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// handleAssetUpload accepts a multipart file upload and writes it to
// data/shared-assets/{folder}/{filename}. Only image MIME types are allowed.
func (s *Server) handleAssetUpload(c echo.Context) error {
	file, header, err := c.Request().FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing file field")
	}
	defer func() { _ = file.Close() }()

	// Validate MIME type.
	contentType := header.Header.Get("Content-Type")
	if !allowedImageMIME[contentType] {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"unsupported file type: "+contentType,
		)
	}

	// Sanitize filename.
	filename := filepath.Base(header.Filename)
	if filename == "." || filename == ".." || filename == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid filename")
	}

	// Sanitize folder.
	folder := c.FormValue("folder")
	if strings.Contains(folder, "..") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid folder path")
	}
	folder = strings.TrimPrefix(filepath.Clean(folder), "/")

	baseDir := filepath.Join(s.DataDir, "shared-assets")
	destDir := filepath.Join(baseDir, folder)

	// Path traversal check.
	cleanDest := filepath.Clean(destDir)
	if !strings.HasPrefix(cleanDest, filepath.Clean(baseDir)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid folder path")
	}

	if mkErr := os.MkdirAll(cleanDest, 0o750); mkErr != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create directory",
		)
	}

	destPath := filepath.Join(cleanDest, filename)

	// Check for existing file — reject duplicates.
	if _, statErr := os.Stat(destPath); statErr == nil {
		return echo.NewHTTPError(http.StatusConflict, "file already exists")
	}

	out, createErr := os.Create(destPath)
	if createErr != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create file",
		)
	}
	defer func() { _ = out.Close() }()

	if _, copyErr := io.Copy(out, file); copyErr != nil {
		// Clean up partial file.
		_ = os.Remove(destPath)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to write file",
		)
	}

	assetPath := filename
	if folder != "" && folder != "." {
		assetPath = folder + "/" + filename
	}

	return c.JSON(http.StatusOK, map[string]string{"path": assetPath})
}
