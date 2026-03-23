package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"go-eino-agent/config"
	"go-eino-agent/internal/handler"
	"go-eino-agent/pkg/logger"
)

type requestMetrics struct {
	startedAt     time.Time
	totalRequests atomic.Uint64
	inFlight      atomic.Int64
	totalLatency  atomic.Int64
	maxLatency    atomic.Int64
	status2xx     atomic.Uint64
	status4xx     atomic.Uint64
	status5xx     atomic.Uint64
	mu            sync.RWMutex
	queryModes    map[string]*queryModeStats
	agentModes    map[string]*agentModeStats
}

type queryModeStats struct {
	Requests       uint64
	Successes      uint64
	ClientErrors   uint64
	ServerErrors   uint64
	TotalLatencyNs int64
	MaxLatencyNs   int64
}

type queryModeMetric struct {
	Requests       uint64  `json:"requests"`
	Successes      uint64  `json:"successes"`
	Failures       uint64  `json:"failures"`
	ClientErrors   uint64  `json:"client_errors"`
	ServerErrors   uint64  `json:"server_errors"`
	FailureRatePct float64 `json:"failure_rate_pct"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	MaxLatencyMs   float64 `json:"max_latency_ms"`
}

type queryModeRank struct {
	Rank           int     `json:"rank"`
	Mode           string  `json:"mode"`
	Requests       uint64  `json:"requests"`
	Successes      uint64  `json:"successes"`
	Failures       uint64  `json:"failures"`
	FailureRatePct float64 `json:"failure_rate_pct"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	MaxLatencyMs   float64 `json:"max_latency_ms"`
}

type agentModeStats struct {
	Requests           uint64
	Fallbacks          uint64
	ToolCalls          uint64
	ToolFailures       uint64
	ValidationFailures uint64
}

type agentModeMetric struct {
	Requests           uint64  `json:"requests"`
	Fallbacks          uint64  `json:"fallbacks"`
	ToolCalls          uint64  `json:"tool_calls"`
	ToolFailures       uint64  `json:"tool_failures"`
	ValidationFailures uint64  `json:"validation_failures"`
	FallbackRatePct    float64 `json:"fallback_rate_pct"`
	ToolFailureRatePct float64 `json:"tool_failure_rate_pct"`
}

func newRequestMetrics() *requestMetrics {
	return &requestMetrics{
		startedAt:  time.Now(),
		queryModes: make(map[string]*queryModeStats),
		agentModes: make(map[string]*agentModeStats),
	}
}

func (m *requestMetrics) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		begin := time.Now()
		m.totalRequests.Add(1)
		m.inFlight.Add(1)
		defer m.inFlight.Add(-1)

		c.Next()

		elapsed := time.Since(begin)
		m.totalLatency.Add(elapsed.Nanoseconds())
		updateMax(&m.maxLatency, elapsed.Nanoseconds())

		status := c.Writer.Status()
		switch {
		case status >= 500:
			m.status5xx.Add(1)
		case status >= 400:
			m.status4xx.Add(1)
		default:
			m.status2xx.Add(1)
		}

		if c.FullPath() == "/api/query" {
			mode := strings.TrimSpace(c.GetString(handler.QueryModeContextKey))
			if mode == "" {
				mode = "unknown"
			}
			m.observeQueryMode(mode, status, elapsed)
			m.observeAgentMode(c, mode)
		}

		logger.Info("[HTTP]",
			zap.String("method", c.Request.Method),
			zap.String("path", c.FullPath()),
			zap.Int("status", status),
			zap.Int64("latency_ms", elapsed.Milliseconds()),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}

func (m *requestMetrics) handleMetrics(c *gin.Context) {
	total := m.totalRequests.Load()
	avgLatencyMs := float64(0)
	if total > 0 {
		avgLatencyMs = float64(m.totalLatency.Load()) / float64(total) / float64(time.Millisecond)
	}

	queryModes := m.snapshotQueryModes()
	c.JSON(http.StatusOK, gin.H{
		"uptime_sec":         int64(time.Since(m.startedAt).Seconds()),
		"total_requests":     total,
		"in_flight":          m.inFlight.Load(),
		"status_2xx":         m.status2xx.Load(),
		"status_4xx":         m.status4xx.Load(),
		"status_5xx":         m.status5xx.Load(),
		"avg_latency_ms":     avgLatencyMs,
		"max_latency_ms":     float64(m.maxLatency.Load()) / float64(time.Millisecond),
		"query_modes":        queryModes,
		"query_modes_ranked": rankQueryModes(queryModes),
		"slowest_query_mode": slowestQueryMode(queryModes),
		"agent_modes":        m.snapshotAgentModes(),
		"timestamp_unix":     time.Now().Unix(),
		"timestamp_rfc3339":  time.Now().Format(time.RFC3339),
	})
}

func (m *requestMetrics) observeQueryMode(mode string, status int, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, ok := m.queryModes[mode]
	if !ok {
		stats = &queryModeStats{}
		m.queryModes[mode] = stats
	}

	stats.Requests++
	stats.TotalLatencyNs += elapsed.Nanoseconds()
	if elapsed.Nanoseconds() > stats.MaxLatencyNs {
		stats.MaxLatencyNs = elapsed.Nanoseconds()
	}

	switch {
	case status >= 500:
		stats.ServerErrors++
	case status >= 400:
		stats.ClientErrors++
	default:
		stats.Successes++
	}
}

func (m *requestMetrics) snapshotQueryModes() map[string]queryModeMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	modes := make([]string, 0, len(m.queryModes))
	for mode := range m.queryModes {
		modes = append(modes, mode)
	}
	sort.Strings(modes)

	result := make(map[string]queryModeMetric, len(modes))
	for _, mode := range modes {
		stats := m.queryModes[mode]
		failures := stats.ClientErrors + stats.ServerErrors
		avgLatencyMs := float64(0)
		if stats.Requests > 0 {
			avgLatencyMs = float64(stats.TotalLatencyNs) / float64(stats.Requests) / float64(time.Millisecond)
		}
		result[mode] = queryModeMetric{
			Requests:       stats.Requests,
			Successes:      stats.Successes,
			Failures:       failures,
			ClientErrors:   stats.ClientErrors,
			ServerErrors:   stats.ServerErrors,
			FailureRatePct: percentageUint64(failures, stats.Requests),
			AvgLatencyMs:   avgLatencyMs,
			MaxLatencyMs:   float64(stats.MaxLatencyNs) / float64(time.Millisecond),
		}
	}
	return result
}

func rankQueryModes(metrics map[string]queryModeMetric) []queryModeRank {
	modes := make([]queryModeRank, 0, len(metrics))
	for mode, metric := range metrics {
		modes = append(modes, queryModeRank{
			Mode:           mode,
			Requests:       metric.Requests,
			Successes:      metric.Successes,
			Failures:       metric.Failures,
			FailureRatePct: metric.FailureRatePct,
			AvgLatencyMs:   metric.AvgLatencyMs,
			MaxLatencyMs:   metric.MaxLatencyMs,
		})
	}
	sort.Slice(modes, func(i, j int) bool {
		if modes[i].AvgLatencyMs == modes[j].AvgLatencyMs {
			if modes[i].FailureRatePct == modes[j].FailureRatePct {
				return modes[i].Requests > modes[j].Requests
			}
			return modes[i].FailureRatePct > modes[j].FailureRatePct
		}
		return modes[i].AvgLatencyMs > modes[j].AvgLatencyMs
	})
	for idx := range modes {
		modes[idx].Rank = idx + 1
	}
	return modes
}

func slowestQueryMode(metrics map[string]queryModeMetric) gin.H {
	ranked := rankQueryModes(metrics)
	if len(ranked) == 0 {
		return gin.H{}
	}
	top := ranked[0]
	return gin.H{
		"mode":             top.Mode,
		"avg_latency_ms":   top.AvgLatencyMs,
		"failure_rate_pct": top.FailureRatePct,
		"requests":         top.Requests,
	}
}

func (m *requestMetrics) observeAgentMode(c *gin.Context, mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, ok := m.agentModes[mode]
	if !ok {
		stats = &agentModeStats{}
		m.agentModes[mode] = stats
	}
	stats.Requests++
	if c.GetBool(handler.QueryFallbackUsedContextKey) {
		stats.Fallbacks++
	}
	stats.ToolCalls += uint64(getIntValue(c, handler.QueryToolCallsContextKey))
	stats.ToolFailures += uint64(getIntValue(c, handler.QueryToolFailuresContextKey))
	stats.ValidationFailures += uint64(getIntValue(c, handler.QueryValidationFailuresContextKey))
}

func (m *requestMetrics) snapshotAgentModes() map[string]agentModeMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]agentModeMetric, len(m.agentModes))
	for mode, stats := range m.agentModes {
		result[mode] = agentModeMetric{
			Requests:           stats.Requests,
			Fallbacks:          stats.Fallbacks,
			ToolCalls:          stats.ToolCalls,
			ToolFailures:       stats.ToolFailures,
			ValidationFailures: stats.ValidationFailures,
			FallbackRatePct:    percentageUint64(stats.Fallbacks, stats.Requests),
			ToolFailureRatePct: percentageUint64(stats.ToolFailures, stats.ToolCalls),
		}
	}
	return result
}

func getIntValue(c *gin.Context, key string) int {
	value, ok := c.Get(key)
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func buildCORSMiddleware(cfg *config.Config) gin.HandlerFunc {
	allowAll := contains(cfg.Server.CORSAllowOrigins, "*")
	allowSet := make(map[string]struct{}, len(cfg.Server.CORSAllowOrigins))
	for _, origin := range cfg.Server.CORSAllowOrigins {
		allowSet[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if allowAll {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			if _, ok := allowSet[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Vary", "Origin")
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rps      float64
	burst    float64
}

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

func newRateLimiter(rps, burst float64) *rateLimiter {
	return &rateLimiter{
		visitors: make(map[string]*visitor),
		rps:      rps,
		burst:    burst,
	}
}

func (rl *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := clientKey(c)
		if !rl.allow(ip, time.Now()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "请求过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}

func (rl *rateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[key]
	if !ok {
		rl.visitors[key] = &visitor{tokens: rl.burst - 1, lastSeen: now}
		rl.cleanupLocked(now)
		return true
	}

	elapsedSec := now.Sub(v.lastSeen).Seconds()
	v.tokens = minFloat(rl.burst, v.tokens+elapsedSec*rl.rps)
	v.lastSeen = now
	if v.tokens < 1 {
		return false
	}
	v.tokens -= 1
	rl.cleanupLocked(now)
	return true
}

func (rl *rateLimiter) cleanupLocked(now time.Time) {
	if len(rl.visitors) < 2048 {
		return
	}
	ttl := 10 * time.Minute
	for key, v := range rl.visitors {
		if now.Sub(v.lastSeen) > ttl {
			delete(rl.visitors, key)
		}
	}
}

func clientKey(c *gin.Context) string {
	if ip := strings.TrimSpace(c.ClientIP()); ip != "" {
		return ip
	}
	return fmt.Sprintf("unknown:%s", c.Request.RemoteAddr)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func updateMax(target *atomic.Int64, val int64) {
	for {
		cur := target.Load()
		if cur >= val {
			return
		}
		if target.CompareAndSwap(cur, val) {
			return
		}
	}
}

func percentageUint64(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}
