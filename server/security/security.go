package security

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrPathEscape = errors.New("path traversal detected: path escapes root boundary")
	safeFileRegex = regexp.MustCompile(`[^\w\s\-\.\(\)]`)
)

func SafeJoin(root, subPath string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(subPath) || filepath.VolumeName(subPath) != "" || strings.HasPrefix(subPath, "/") || strings.HasPrefix(subPath, "\\") {
		return "", ErrPathEscape
	}

	joined := filepath.Join(cleanRoot, subPath)
	cleanJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(cleanRoot, cleanJoined)
	if err != nil || strings.HasPrefix(rel, "..") || (rel == "." && cleanJoined != cleanRoot) {
		return "", ErrPathEscape
	}

	return cleanJoined, nil
}

func SanitizeFilename(name string) string {
	cleaned := safeFileRegex.ReplaceAllString(name, "_")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "Unknown"
	}
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	return cleaned
}
