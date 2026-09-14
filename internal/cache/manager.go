package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type CacheItem struct {
	Key          string    `json:"key"`
	FilePath     string    `json:"file_path"`
	Size         int64     `json:"size"`
	LastAccessed time.Time `json:"last_accessed"`
	Pinned       bool      `json:"pinned"`
}

type Manager struct {
	mu        sync.RWMutex
	dir       string
	maxSizeMB int64
	items     map[string]*CacheItem
}

func NewManager(dir string, maxSizeMB int64) (*Manager, error) {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".rhythm", "cache")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	if maxSizeMB <= 0 {
		maxSizeMB = 1024
	}

	m := &Manager{
		dir:       dir,
		maxSizeMB: maxSizeMB,
		items:     make(map[string]*CacheItem),
	}

	m.scanExisting()
	return m, nil
}

func (m *Manager) scanExisting() {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}

		key := entry.Name()
		m.items[key] = &CacheItem{
			Key:          key,
			FilePath:     filepath.Join(m.dir, key),
			Size:         info.Size(),
			LastAccessed: info.ModTime(),
			Pinned:       false,
		}
	}
}

func (m *Manager) HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:16])
}

func (m *Manager) Get(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hashed := m.HashKey(key)
	item, ok := m.items[hashed]
	if !ok {
		return "", false
	}

	if _, err := os.Stat(item.FilePath); err != nil {
		delete(m.items, hashed)
		return "", false
	}

	item.LastAccessed = time.Now()
	_ = os.Chtimes(item.FilePath, item.LastAccessed, item.LastAccessed)
	return item.FilePath, true
}

func (m *Manager) Put(key string, reader io.Reader, pinned bool) (string, error) {
	hashed := m.HashKey(key)
	destPath := filepath.Join(m.dir, hashed)
	tempPath := destPath + ".tmp"

	outFile, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}

	written, err := io.Copy(outFile, reader)
	outFile.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	if err := os.Rename(tempPath, destPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	m.mu.Lock()
	m.items[hashed] = &CacheItem{
		Key:          hashed,
		FilePath:     destPath,
		Size:         written,
		LastAccessed: time.Now(),
		Pinned:       pinned,
	}
	m.mu.Unlock()

	m.evictIfNeeded()
	return destPath, nil
}

func (m *Manager) Pin(key string, pinned bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	hashed := m.HashKey(key)
	if item, ok := m.items[hashed]; ok {
		item.Pinned = pinned
		return true
	}
	return false
}

func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	defer m.mu.Unlock()

	maxBytes := m.maxSizeMB * 1024 * 1024
	var totalBytes int64
	for _, it := range m.items {
		totalBytes += it.Size
	}

	if totalBytes <= maxBytes {
		return
	}

	var evictable []*CacheItem
	for _, it := range m.items {
		if !it.Pinned {
			evictable = append(evictable, it)
		}
	}

	sort.Slice(evictable, func(i, j int) bool {
		return evictable[i].LastAccessed.Before(evictable[j].LastAccessed)
	})

	for _, it := range evictable {
		if totalBytes <= maxBytes {
			break
		}
		_ = os.Remove(it.FilePath)
		totalBytes -= it.Size
		delete(m.items, it.Key)
	}
}

func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k, it := range m.items {
		if !it.Pinned {
			_ = os.Remove(it.FilePath)
			delete(m.items, k)
		}
	}
	return nil
}

func (m *Manager) Stats() (usedBytes int64, totalItems int, pinnedItems int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, it := range m.items {
		usedBytes += it.Size
		totalItems++
		if it.Pinned {
			pinnedItems++
		}
	}
	return
}

func (m *Manager) FormattedStats() string {
	usedBytes, total, pinned := m.Stats()
	usedMB := float64(usedBytes) / (1024 * 1024)
	return fmt.Sprintf("%.1f MB / %d MB (%d items, %d pinned)", usedMB, m.maxSizeMB, total, pinned)
}
