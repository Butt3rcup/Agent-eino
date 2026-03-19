package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

func sanitizeUploadFilename(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "" || base == "." {
		return "upload_file"
	}

	re := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	safe := re.ReplaceAllString(base, "_")
	safe = strings.Trim(safe, " .")
	if safe == "" {
		return "upload_file"
	}

	const maxFilenameLen = 128
	runes := []rune(safe)
	if len(runes) > maxFilenameLen {
		safe = string(runes[:maxFilenameLen])
	}

	return safe
}

func detectUploadedContentType(fileHeader *multipart.FileHeader) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return http.DetectContentType(header[:n]), nil
}

func isAllowedUploadType(ext, contentType string) bool {
	switch ext {
	case ".pdf":
		return contentType == "application/pdf"
	case ".md", ".markdown":
		return strings.HasPrefix(contentType, "text/") ||
			contentType == "application/octet-stream" ||
			contentType == "application/x-empty"
	default:
		return false
	}
}
