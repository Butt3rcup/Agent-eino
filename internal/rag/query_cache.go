package rag

import "go-eino-agent/internal/cache"

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

func metricFromCache(provider cache.MetricsProvider) QueryCacheMetric {
	if provider == nil {
		return QueryCacheMetric{Enabled: false}
	}
	stats := provider.Stats()
	if stats.Capacity <= 0 || stats.TTL <= 0 {
		return QueryCacheMetric{Enabled: false}
	}
	return QueryCacheMetric{
		Enabled:  true,
		Entries:  stats.Entries,
		Capacity: stats.Capacity,
		TTLSec:   int(stats.TTL.Seconds()),
		Hits:     stats.Hits,
		Misses:   stats.Misses,
	}
}
