package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"go-eino-agent/config"
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
}

func newRequestMetrics() *requestMetrics {
	return &requestMetrics{startedAt: time.Now()}
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

	c.JSON(http.StatusOK, gin.H{
		"uptime_sec":        int64(time.Since(m.startedAt).Seconds()),
		"total_requests":    total,
		"in_flight":         m.inFlight.Load(),
		"status_2xx":        m.status2xx.Load(),
		"status_4xx":        m.status4xx.Load(),
		"status_5xx":        m.status5xx.Load(),
		"avg_latency_ms":    avgLatencyMs,
		"max_latency_ms":    float64(m.maxLatency.Load()) / float64(time.Millisecond),
		"timestamp_unix":    time.Now().Unix(),
		"timestamp_rfc3339": time.Now().Format(time.RFC3339),
	})
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
