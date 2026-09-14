package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"rhythm/internal/core"
	"rhythm/internal/metadata"
	"rhythm/internal/storage"
)

type ScanStats struct {
	TotalScanned int
	NewAdded     int
	Updated      int
	Errors       int
}

type Manager struct {
	db        *storage.DB
	extractor *metadata.Extractor
	mu        sync.RWMutex
	scanning  bool
}

func NewManager(db *storage.DB) *Manager {
	return &Manager{
		db:        db,
		extractor: metadata.NewExtractor(),
	}
}

var SupportedExtensions = map[string]bool{
	".mp3":  true,
	".flac": true,
	".wav":  true,
	".ogg":  true,
	".m4a":  true,
	".aac":  true,
}

func (m *Manager) ScanDirectories(paths []string) (*ScanStats, error) {
	m.mu.Lock()
	if m.scanning {
		m.mu.Unlock()
		return nil, fmt.Errorf("scan already in progress")
	}
	m.scanning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.scanning = false
		m.mu.Unlock()
	}()

	stats := &ScanStats{}

	existing, _ := m.db.GetAllTracks()
	existingPaths := make(map[string]core.Track)
	for _, t := range existing {
		if t.LocalPath != "" {
			existingPaths[t.LocalPath] = t
		}
	}

	for _, root := range paths {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}

		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(path))
			if !SupportedExtensions[ext] {
				return nil
			}

			stats.TotalScanned++

			if oldTrack, exists := existingPaths[path]; exists {
				info, err := d.Info()
				if err == nil && info.Size() == oldTrack.FileSize {

					return nil
				}
				stats.Updated++
			} else {
				stats.NewAdded++
			}

			track, err := m.extractor.Extract(path)
			if err != nil {
				stats.Errors++
				return nil
			}

			if err := m.db.SaveTrack(track); err != nil {
				stats.Errors++
			}

			return nil
		})
	}

	return stats, nil
}

func (m *Manager) Search(query string) ([]core.Track, error) {
	return m.db.SearchTracks(query)
}

func (m *Manager) AllTracks() ([]core.Track, error) {
	return m.db.GetAllTracks()
}

func (m *Manager) Artists() ([]string, error) {
	return m.db.GetArtists()
}

func (m *Manager) Albums() ([]string, error) {
	return m.db.GetAlbums()
}

func (m *Manager) IsScanning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scanning
}
