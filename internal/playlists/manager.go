package playlists

import (
	"fmt"
	"sync"
	"time"

	"rhythm/internal/core"
	"github.com/google/uuid"
)

type Manager struct {
	mu        sync.RWMutex
	playlists map[string]*core.Playlist
	storage   Storage
}

type Storage interface {
	SavePlaylist(p *core.Playlist) error
	DeletePlaylist(id string) error
	GetPlaylists() ([]core.Playlist, error)
}

func NewManager(s Storage) *Manager {
	m := &Manager{
		playlists: make(map[string]*core.Playlist),
		storage:   s,
	}
	if s != nil {
		if lists, err := s.GetPlaylists(); err == nil {
			for i := range lists {
				p := lists[i]
				m.playlists[p.ID] = &p
			}
		}
	}
	return m
}

func (m *Manager) Create(name, description string) (*core.Playlist, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p := &core.Playlist{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		Tracks:      make([]core.Track, 0),
	}

	m.playlists[p.ID] = p
	if m.storage != nil {
		if err := m.storage.SavePlaylist(p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (m *Manager) Get(id string) (*core.Playlist, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.playlists[id]
	return p, ok
}

func (m *Manager) List() []core.Playlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]core.Playlist, 0, len(m.playlists))
	for _, p := range m.playlists {
		res = append(res, *p)
	}
	return res
}

func (m *Manager) AddTrack(playlistID string, track core.Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.playlists[playlistID]
	if !ok {
		return fmt.Errorf("playlist not found: %s", playlistID)
	}

	p.Tracks = append(p.Tracks, track)
	if m.storage != nil {
		return m.storage.SavePlaylist(p)
	}
	return nil
}

func (m *Manager) RemoveTrack(playlistID string, idx int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.playlists[playlistID]
	if !ok {
		return fmt.Errorf("playlist not found: %s", playlistID)
	}

	if idx < 0 || idx >= len(p.Tracks) {
		return fmt.Errorf("invalid track index %d", idx)
	}

	p.Tracks = append(p.Tracks[:idx], p.Tracks[idx+1:]...)
	if m.storage != nil {
		return m.storage.SavePlaylist(p)
	}
	return nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.playlists, id)
	if m.storage != nil {
		return m.storage.DeletePlaylist(id)
	}
	return nil
}

func (m *Manager) Rename(id, name, desc string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.playlists[id]
	if !ok {
		return fmt.Errorf("playlist not found: %s", id)
	}

	p.Name = name
	p.Description = desc
	if m.storage != nil {
		return m.storage.SavePlaylist(p)
	}
	return nil
}
