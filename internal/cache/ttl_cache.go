package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics describes the runtime stats of a TTL cache instance.
type Metrics struct {
	Entries  int
	Capacity int
	TTL      time.Duration
	Hits     uint64
	Misses   uint64
}

// MetricsProvider exposes Stats for cache instrumentation.
type MetricsProvider interface {
	Stats() Metrics
}

type ttlCacheEntry[V any] struct {
	value      V
	expiresAt  time.Time
	generation uint64
	updatedAt  time.Time
}

// TTLCache is a bounded map with TTL semantics and optional copy hooks.
type TTLCache[V any] struct {
	mu         sync.RWMutex
	entries    map[string]ttlCacheEntry[V]
	maxEntries int
	ttl        time.Duration
	copyFn     func(V) V
	hits       atomic.Uint64
	misses     atomic.Uint64
}

// NewTTLCache constructs a TTL cache. Returns nil when size or ttl is invalid.
func NewTTLCache[V any](maxEntries int, ttl time.Duration, copyFn func(V) V) *TTLCache[V] {
	if maxEntries <= 0 || ttl <= 0 {
		return nil
	}
	return &TTLCache[V]{
		entries:    make(map[string]ttlCacheEntry[V], maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
		copyFn:     copyFn,
	}
}

// Get returns a copy of the cached item when it exists and is still valid.
func (c *TTLCache[V]) Get(key string, generation uint64) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	now := time.Now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || entry.generation != generation || now.After(entry.expiresAt) {
		c.misses.Add(1)
		return zero, false
	}
	c.hits.Add(1)
	return c.clone(entry.value), true
}

// Set stores the given value and evicts expired/oldest entries when needed.
func (c *TTLCache[V]) Set(key string, value V, generation uint64) {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked(now)
	if len(c.entries) >= c.maxEntries {
		c.evictOldestLocked()
	}
	c.entries[key] = ttlCacheEntry[V]{
		value:      c.clone(value),
		expiresAt:  now.Add(c.ttl),
		generation: generation,
		updatedAt:  now,
	}
}

// Stats returns the cache metrics snapshot.
func (c *TTLCache[V]) Stats() Metrics {
	if c == nil {
		return Metrics{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Metrics{
		Entries:  len(c.entries),
		Capacity: c.maxEntries,
		TTL:      c.ttl,
		Hits:     c.hits.Load(),
		Misses:   c.misses.Load(),
	}
}

func (c *TTLCache[V]) clone(value V) V {
	if c == nil || c.copyFn == nil {
		return value
	}
	return c.copyFn(value)
}

func (c *TTLCache[V]) evictExpiredLocked(now time.Time) {
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

func (c *TTLCache[V]) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, entry := range c.entries {
		if first || entry.updatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.updatedAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
