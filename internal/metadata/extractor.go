package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"rhythm/internal/core"
	"github.com/dhowden/tag"
	"github.com/google/uuid"
)

type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Extract(filePath string) (*core.Track, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	track := &core.Track{
		ID:        uuid.New().String(),
		Source:    core.SourceLocal,
		SourceID:  filePath,
		LocalPath: filePath,
		FileSize:  stat.Size(),
		DateAdded: time.Now(),
		Format:    strings.TrimPrefix(filepath.Ext(filePath), "."),
	}

	m, err := tag.ReadFrom(file)
	if err == nil && m != nil {
		if m.Title() != "" {
			track.Title = m.Title()
		}
		if m.Artist() != "" {
			track.Artist = m.Artist()
		}
		if m.Album() != "" {
			track.Album = m.Album()
		}
		if m.Year() > 0 {
			track.Year = m.Year()
		}
		if m.Genre() != "" {
			track.Genre = m.Genre()
		}
	}

	if track.Title == "" || track.Title == "Unknown Title" {
		base := filepath.Base(filePath)
		ext := filepath.Ext(base)
		track.Title = strings.TrimSuffix(base, ext)
	}

	if track.Artist == "" {
		track.Artist = "Unknown Artist"
	}
	if track.Album == "" {
		track.Album = "Unknown Album"
	}

	return track, nil
}
