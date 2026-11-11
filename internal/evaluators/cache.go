package evaluators

import (
	"container/list"
	"context"
	"sync"
)

// MemoryCache is a bounded, concurrency-safe LRU Cache for dev mode and
// tests. Production deployments plug in a shared cache (internal/cache).
type MemoryCache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List
	items map[string]*list.Element
}

type memEntry struct {
	key string
	val []byte
}

// NewMemoryCache returns an LRU cache holding at most maxEntries values
// (default 10,000 when maxEntries <= 0).
func NewMemoryCache(maxEntries int) *MemoryCache {
	if maxEntries <= 0 {
		maxEntries = 10_000
	}
	return &MemoryCache{max: maxEntries, ll: list.New(), items: map[string]*list.Element{}}
}

func (m *MemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.ll.MoveToFront(el)
		return el.Value.(*memEntry).val, true, nil
	}
	return nil, false, nil
}

func (m *MemoryCache) Set(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		el.Value.(*memEntry).val = value
		m.ll.MoveToFront(el)
		return nil
	}
	m.items[key] = m.ll.PushFront(&memEntry{key: key, val: value})
	for m.ll.Len() > m.max {
		last := m.ll.Back()
		m.ll.Remove(last)
		delete(m.items, last.Value.(*memEntry).key)
	}
	return nil
}

// Len returns the number of cached entries.
func (m *MemoryCache) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ll.Len()
}
