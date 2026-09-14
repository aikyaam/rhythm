package streaming

import (
	"fmt"
	"net/http"
	"os"

	"rhythm/server/security"
)

type Handler struct {
	musicRoot string
}

func NewHandler(musicRoot string) *Handler {
	return &Handler{musicRoot: musicRoot}
}

func (h *Handler) ServeAudio(w http.ResponseWriter, r *http.Request, relPath string) {
	fullPath, err := security.SafeJoin(h.musicRoot, relPath)
	if err != nil {
		http.Error(w, "Access Denied", http.StatusForbidden)
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Track file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to open audio: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to read file info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
}
