package history

import (
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

type Storage interface {
	SaveHistoryEntry(entry *core.HistoryEntry) error
	GetRecentHistory(limit int) ([]core.HistoryEntry, error)
}

type Tracker struct {
	mu      sync.RWMutex
	storage Storage
	recent  []core.HistoryEntry
	maxSize int
}

func NewTracker(s Storage, maxSize int) *Tracker {
	if maxSize <= 0 {
		maxSize = 100
	}
	t := &Tracker{
		storage: s,
		recent:  make([]core.HistoryEntry, 0, maxSize),
		maxSize: maxSize,
	}
	if s != nil {
		if items, err := s.GetRecentHistory(maxSize); err == nil {
			t.recent = items
		}
	}
	return t
}

func (t *Tracker) Record(track core.Track, duration float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := core.HistoryEntry{
		ID:       uuid.New().String(),
		TrackID:  track.ID,
		Track:    track,
		PlayedAt: time.Now(),
		Duration: duration,
	}

	t.recent = append([]core.HistoryEntry{entry}, t.recent...)
	if len(t.recent) > t.maxSize {
		t.recent = t.recent[:t.maxSize]
	}

	if t.storage != nil {
		return t.storage.SaveHistoryEntry(&entry)
	}
	return nil
}

func (t *Tracker) Recent(limit int) []core.HistoryEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.recent) {
		limit = len(t.recent)
	}
	out := make([]core.HistoryEntry, limit)
	copy(out, t.recent[:limit])
	return out
}
