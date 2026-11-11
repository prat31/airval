// Package cache is the response cache used by the execution engine: target responses (opt-in per
// suite) and LLM-judge / embedding results (on by default). It is a two-level cache: a bounded
// in-process LRU in front of the store's cache_entries table, so repeated lookups inside one worker
// never touch the database and every worker shares hits through the store.
//
// Writes to the store are write-behind: Set updates the LRU synchronously and queues the store
// write for a background writer, so hot paths never wait on the database. Keys are opaque; callers
// hash their inputs (see Key).
package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prat31/airval/internal/store"
)

// Options configure a Cache.
type Options struct {
	// MaxEntries bounds the in-process LRU (default 50,000).
	MaxEntries int
	// MaxBytes bounds the LRU's total value size (default 256 MiB).
	MaxBytes int
	// TTL of store entries (0 = no expiry).
	TTL time.Duration
	// WriteQueue is the capacity of the write-behind queue (default 4096). When full, store
	// writes are dropped (the LRU still holds the value).
	WriteQueue int
	Logger     *slog.Logger
}

// Cache is a concurrency-safe two-level cache. It implements evaluators.Cache.
type Cache struct {
	st   store.CacheStore
	opts Options
	log  *slog.Logger

	mu    sync.Mutex
	ll    *list.List
	items map[string]*list.Element
	bytes int

	wmu    sync.RWMutex // guards closed + sends on writes
	closed bool
	writes chan write
	wg     sync.WaitGroup
	once   sync.Once

	hits, misses, storeHits atomic.Int64
}

type entry struct {
	key string
	val []byte
}

type write struct {
	key string
	val []byte
}

// New creates a cache backed by st (st may be nil for a memory-only cache). Call Close to flush
// pending writes.
func New(st store.CacheStore, o Options) *Cache {
	if o.MaxEntries <= 0 {
		o.MaxEntries = 50_000
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 256 << 20
	}
	if o.WriteQueue <= 0 {
		o.WriteQueue = 4096
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	c := &Cache{st: st, opts: o, log: o.Logger, ll: list.New(), items: map[string]*list.Element{}}
	if st != nil {
		c.writes = make(chan write, o.WriteQueue)
		c.wg.Add(1)
		go c.writer()
	}
	return c
}

// Key hashes parts into a namespaced cache key: "<ns>:<hex sha256>".
func Key(ns string, parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		var n [8]byte
		l := len(p)
		for i := 0; i < 8; i++ {
			n[i] = byte(l >> (8 * i))
		}
		h.Write(n[:]) // length-prefix so ("ab","c") != ("a","bc")
		h.Write(p)
	}
	return ns + ":" + hex.EncodeToString(h.Sum(nil))
}

// Get returns a cached value. A miss is (nil, false, nil).
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if v, ok := c.getLocal(key); ok {
		c.hits.Add(1)
		return v, true, nil
	}
	if c.st == nil {
		c.misses.Add(1)
		return nil, false, nil
	}
	v, ok, err := c.st.CacheGet(ctx, key)
	if err != nil || !ok {
		c.misses.Add(1)
		return nil, false, err
	}
	c.storeHits.Add(1)
	c.hits.Add(1)
	c.putLocal(key, v)
	return v, true, nil
}

// Set stores a value in the LRU and (asynchronously) in the store.
func (c *Cache) Set(_ context.Context, key string, value []byte) error {
	c.putLocal(key, value)
	if c.writes == nil {
		return nil
	}
	c.wmu.RLock()
	defer c.wmu.RUnlock()
	if c.closed {
		return nil
	}
	select {
	case c.writes <- write{key, value}:
	default:
		c.log.Debug("cache: write queue full, dropping store write", "key", key)
	}
	return nil
}

func (c *Cache) getLocal(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry).val, true
	}
	return nil, false
}

func (c *Cache) putLocal(key string, val []byte) {
	if len(val) > c.opts.MaxBytes/4 {
		return // too large for the front cache
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		c.bytes += len(val) - len(e.val)
		e.val = val
		c.ll.MoveToFront(el)
	} else {
		c.items[key] = c.ll.PushFront(&entry{key: key, val: val})
		c.bytes += len(val)
	}
	for c.ll.Len() > c.opts.MaxEntries || c.bytes > c.opts.MaxBytes {
		last := c.ll.Back()
		e := last.Value.(*entry)
		c.ll.Remove(last)
		delete(c.items, e.key)
		c.bytes -= len(e.val)
	}
}

func (c *Cache) writer() {
	defer c.wg.Done()
	for w := range c.writes {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := c.st.CachePut(ctx, w.key, w.val, c.opts.TTL); err != nil {
			c.log.Warn("cache: store write failed", "err", err)
		}
		cancel()
	}
}

// Stats reports hit/miss counters (hits include store hits).
func (c *Cache) Stats() (hits, misses, storeHits int64) {
	return c.hits.Load(), c.misses.Load(), c.storeHits.Load()
}

// Len returns the number of entries in the in-process LRU.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Close flushes queued store writes and stops the writer. Reads keep working; later writes only
// update the in-process LRU.
func (c *Cache) Close() {
	c.once.Do(func() {
		if c.writes != nil {
			c.wmu.Lock()
			c.closed = true
			close(c.writes)
			c.wmu.Unlock()
			c.wg.Wait()
		}
	})
}
