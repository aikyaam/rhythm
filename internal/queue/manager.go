package queue

import (
	"math/rand"
	"sync"
	"time"

	"rhythm/internal/core"
)

type Manager struct {
	mu           sync.RWMutex
	items        []core.QueueItem
	currentIndex int
	repeatMode   core.RepeatMode
	isShuffled   bool
	original     []core.QueueItem
}

func NewManager() *Manager {
	return &Manager{
		items:        make([]core.QueueItem, 0),
		currentIndex: -1,
		repeatMode:   core.RepeatOff,
		isShuffled:   false,
		original:     make([]core.QueueItem, 0),
	}
}

func (m *Manager) Add(tracks ...core.Track) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range tracks {
		item := core.QueueItem{
			ID:      t.ID,
			Track:   t,
			AddedAt: time.Now(),
		}
		m.items = append(m.items, item)
		m.original = append(m.original, item)
	}

	if m.currentIndex == -1 && len(m.items) > 0 {
		m.currentIndex = 0
	}
}

func (m *Manager) PlayNext(tracks ...core.Track) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.items) == 0 || m.currentIndex < 0 {
		m.mu.Unlock()
		m.Add(tracks...)
		m.mu.Lock()
		return
	}

	insertIdx := m.currentIndex + 1
	var newItems []core.QueueItem
	for _, t := range tracks {
		newItems = append(newItems, core.QueueItem{
			ID:      t.ID,
			Track:   t,
			AddedAt: time.Now(),
		})
	}

	tail := append([]core.QueueItem{}, m.items[insertIdx:]...)
	m.items = append(append(m.items[:insertIdx:insertIdx], newItems...), tail...)

	origTail := append([]core.QueueItem{}, m.original[insertIdx:]...)
	m.original = append(append(m.original[:insertIdx:insertIdx], newItems...), origTail...)
}

func (m *Manager) Current() *core.Track {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentIndex >= 0 && m.currentIndex < len(m.items) {
		t := m.items[m.currentIndex].Track
		return &t
	}
	return nil
}

func (m *Manager) CurrentIndex() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentIndex
}

func (m *Manager) Next() *core.Track {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.items) == 0 {
		return nil
	}

	if m.repeatMode == core.RepeatOne && m.currentIndex >= 0 {
		t := m.items[m.currentIndex].Track
		return &t
	}

	if m.currentIndex+1 < len(m.items) {
		m.currentIndex++
		t := m.items[m.currentIndex].Track
		return &t
	}

	if m.repeatMode == core.RepeatAll {
		m.currentIndex = 0
		t := m.items[m.currentIndex].Track
		return &t
	}

	return nil
}

func (m *Manager) Previous() *core.Track {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.items) == 0 {
		return nil
	}

	if m.currentIndex > 0 {
		m.currentIndex--
		t := m.items[m.currentIndex].Track
		return &t
	}

	if m.repeatMode == core.RepeatAll && len(m.items) > 0 {
		m.currentIndex = len(m.items) - 1
		t := m.items[m.currentIndex].Track
		return &t
	}

	if m.currentIndex == 0 {
		t := m.items[0].Track
		return &t
	}

	return nil
}

func (m *Manager) JumpTo(idx int) *core.Track {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx >= 0 && idx < len(m.items) {
		m.currentIndex = idx
		t := m.items[m.currentIndex].Track
		return &t
	}
	return nil
}

func (m *Manager) Remove(idx int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx < 0 || idx >= len(m.items) {
		return
	}

	m.items = append(m.items[:idx], m.items[idx+1:]...)
	if idx < m.currentIndex {
		m.currentIndex--
	} else if m.currentIndex >= len(m.items) {
		m.currentIndex = len(m.items) - 1
	}
}

func (m *Manager) Move(fromIdx, toIdx int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if fromIdx < 0 || fromIdx >= len(m.items) || toIdx < 0 || toIdx >= len(m.items) || fromIdx == toIdx {
		return
	}

	item := m.items[fromIdx]
	newItems := make([]core.QueueItem, 0, len(m.items))
	for i, it := range m.items {
		if i != fromIdx {
			newItems = append(newItems, it)
		}
	}

	if toIdx >= len(newItems) {
		newItems = append(newItems, item)
	} else {
		tail := append([]core.QueueItem{item}, newItems[toIdx:]...)
		newItems = append(newItems[:toIdx], tail...)
	}
	m.items = newItems

	if m.currentIndex == fromIdx {
		m.currentIndex = toIdx
	} else if fromIdx < m.currentIndex && toIdx >= m.currentIndex {
		m.currentIndex--
	} else if fromIdx > m.currentIndex && toIdx <= m.currentIndex {
		m.currentIndex++
	}
}

func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = make([]core.QueueItem, 0)
	m.original = make([]core.QueueItem, 0)
	m.currentIndex = -1
	m.isShuffled = false
}

func (m *Manager) ToggleShuffle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.items) <= 1 {
		m.isShuffled = !m.isShuffled
		return m.isShuffled
	}

	if !m.isShuffled {
		var currentItem *core.QueueItem
		if m.currentIndex >= 0 && m.currentIndex < len(m.items) {
			item := m.items[m.currentIndex]
			currentItem = &item
		}

		shuffled := make([]core.QueueItem, len(m.items))
		copy(shuffled, m.items)

		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		if currentItem != nil {
			for i, it := range shuffled {
				if it.ID == currentItem.ID {
					shuffled[i], shuffled[0] = shuffled[0], shuffled[i]
					break
				}
			}
			m.currentIndex = 0
		}

		m.items = shuffled
		m.isShuffled = true
	} else {
		var currentID string
		if m.currentIndex >= 0 && m.currentIndex < len(m.items) {
			currentID = m.items[m.currentIndex].ID
		}

		m.items = make([]core.QueueItem, len(m.original))
		copy(m.items, m.original)

		if currentID != "" {
			for i, it := range m.items {
				if it.ID == currentID {
					m.currentIndex = i
					break
				}
			}
		}
		m.isShuffled = false
	}

	return m.isShuffled
}

func (m *Manager) CycleRepeat() core.RepeatMode {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.repeatMode {
	case core.RepeatOff:
		m.repeatMode = core.RepeatAll
	case core.RepeatAll:
		m.repeatMode = core.RepeatOne
	case core.RepeatOne:
		m.repeatMode = core.RepeatOff
	default:
		m.repeatMode = core.RepeatOff
	}
	return m.repeatMode
}

func (m *Manager) SetRepeatMode(mode core.RepeatMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repeatMode = mode
}

func (m *Manager) Items() []core.QueueItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]core.QueueItem, len(m.items))
	copy(out, m.items)
	return out
}

func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

func (m *Manager) IsShuffled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isShuffled
}

func (m *Manager) RepeatMode() core.RepeatMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.repeatMode
}
