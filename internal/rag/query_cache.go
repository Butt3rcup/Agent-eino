package rag

import (
	"sync"
	"sync/atomic"
	"time"
)

type QueryCacheMetric struct {
	Enabled  bool   `json:"enabled"`
	Entries  int    `json:"entries"`
	Capacity int    `json:"capacity"`
	TTLSec   int    `json:"ttl_sec"`
	Hits     uint64 `json:"hits"`
	Misses   uint64 `json:"misses"`
}

type QueryCacheStats struct {
	Embedding QueryCacheMetric `json:"embedding"`
	Search    QueryCacheMetric `json:"search"`
	Context   QueryCacheMetric `json:"context"`
}

type ttlCacheEntry[V any] struct {
	value      V
	expiresAt  time.Time
	generation uint64
	updatedAt  time.Time
}

type ttlCache[V any] struct {
	mu         sync.RWMutex
	entries    map[string]ttlCacheEntry[V]
	maxEntries int
	ttl        time.Duration
	copyFn     func(V) V
	hits       atomic.Uint64
	misses     atomic.Uint64
}

func newTTLCache[V any](maxEntries int, ttl time.Duration, copyFn func(V) V) *ttlCache[V] {
	if maxEntries <= 0 || ttl <= 0 {
		return nil
	}
	return &ttlCache[V]{
		entries:    make(map[string]ttlCacheEntry[V], maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
		copyFn:     copyFn,
	}
}

func (c *ttlCache[V]) Get(key string, generation uint64) (V, bool) {
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
		if ok && now.After(entry.expiresAt) {
			c.mu.Lock()
			delete(c.entries, key)
			c.mu.Unlock()
		}
		return zero, false
	}
	c.hits.Add(1)
	return c.clone(entry.value), true
}

func (c *ttlCache[V]) Set(key string, value V, generation uint64) {
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

func (c *ttlCache[V]) Stats() QueryCacheMetric {
	if c == nil {
		return QueryCacheMetric{Enabled: false}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return QueryCacheMetric{
		Enabled:  true,
		Entries:  len(c.entries),
		Capacity: c.maxEntries,
		TTLSec:   int(c.ttl.Seconds()),
		Hits:     c.hits.Load(),
		Misses:   c.misses.Load(),
	}
}

func (c *ttlCache[V]) clone(value V) V {
	if c == nil || c.copyFn == nil {
		return value
	}
	return c.copyFn(value)
}

func (c *ttlCache[V]) evictExpiredLocked(now time.Time) {
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

func (c *ttlCache[V]) evictOldestLocked() {
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
