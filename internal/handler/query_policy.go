package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go-eino-agent/config"
)

type QueryModePolicySnapshot struct {
	TimeoutSec     int     `json:"timeout_sec"`
	RateLimitRPS   float64 `json:"rate_limit_rps"`
	RateLimitBurst float64 `json:"rate_limit_burst"`
}

type queryModePolicy struct {
	TimeoutSec     int
	RateLimitRPS   float64
	RateLimitBurst float64
	Limiter        *clientRateLimiter
}

type clientRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*clientVisitor
	rps      float64
	burst    float64
}

type clientVisitor struct {
	tokens   float64
	lastSeen time.Time
}

var builtInQueryModes = []string{
	"rag",
	"react",
	"rag_agent",
	"multi-agent",
	"graph_rag",
	"graph_multi",
}

func buildQueryModePolicies(cfg *config.Config) map[string]queryModePolicy {
	modeSet := make(map[string]struct{}, len(builtInQueryModes)+len(cfg.Security.QueryModeTimeoutSec))
	for _, mode := range builtInQueryModes {
		modeSet[mode] = struct{}{}
	}
	for mode := range cfg.Security.QueryModeTimeoutSec {
		if strings.TrimSpace(mode) != "" {
			modeSet[mode] = struct{}{}
		}
	}
	for mode := range cfg.Security.QueryModeRateLimitRPS {
		if strings.TrimSpace(mode) != "" {
			modeSet[mode] = struct{}{}
		}
	}
	for mode := range cfg.Security.QueryModeRateLimitBurst {
		if strings.TrimSpace(mode) != "" {
			modeSet[mode] = struct{}{}
		}
	}

	policies := make(map[string]queryModePolicy, len(modeSet))
	for mode := range modeSet {
		timeoutSec := cfg.Security.QueryDefaultTimeoutSec
		if value, ok := cfg.Security.QueryModeTimeoutSec[mode]; ok {
			timeoutSec = value
		}
		rateLimitRPS := cfg.Security.QueryDefaultRateLimitRPS
		if value, ok := cfg.Security.QueryModeRateLimitRPS[mode]; ok {
			rateLimitRPS = value
		}
		rateLimitBurst := cfg.Security.QueryDefaultRateLimitBurst
		if value, ok := cfg.Security.QueryModeRateLimitBurst[mode]; ok {
			rateLimitBurst = value
		}
		policies[mode] = queryModePolicy{
			TimeoutSec:     timeoutSec,
			RateLimitRPS:   rateLimitRPS,
			RateLimitBurst: rateLimitBurst,
			Limiter:        newClientRateLimiter(rateLimitRPS, rateLimitBurst),
		}
	}
	return policies
}

func (h *Handler) applyQueryPolicy(c *gin.Context, mode string) (context.CancelFunc, bool) {
	policy, ok := h.queryPolicies[mode]
	if !ok {
		return func() {}, true
	}
	if policy.Limiter != nil && !policy.Limiter.Allow(queryClientKey(c), time.Now()) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("模式 %s 请求过于频繁，请稍后再试", mode),
		})
		return nil, false
	}
	if policy.TimeoutSec <= 0 {
		return func() {}, true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(policy.TimeoutSec)*time.Second)
	c.Request = c.Request.WithContext(ctx)
	c.Set("query_timeout_sec", policy.TimeoutSec)
	return cancel, true
}

func (h *Handler) QueryPolicySnapshot() map[string]QueryModePolicySnapshot {
	snapshot := make(map[string]QueryModePolicySnapshot, len(h.queryPolicies))
	for mode, policy := range h.queryPolicies {
		snapshot[mode] = QueryModePolicySnapshot{
			TimeoutSec:     policy.TimeoutSec,
			RateLimitRPS:   policy.RateLimitRPS,
			RateLimitBurst: policy.RateLimitBurst,
		}
	}
	return snapshot
}

func newClientRateLimiter(rps, burst float64) *clientRateLimiter {
	if rps <= 0 || burst < 1 {
		return nil
	}
	return &clientRateLimiter{
		visitors: make(map[string]*clientVisitor),
		rps:      rps,
		burst:    burst,
	}
}

func (rl *clientRateLimiter) Allow(key string, now time.Time) bool {
	if rl == nil {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	visitorState, ok := rl.visitors[key]
	if !ok {
		rl.visitors[key] = &clientVisitor{tokens: rl.burst - 1, lastSeen: now}
		rl.cleanupLocked(now)
		return true
	}

	elapsedSec := now.Sub(visitorState.lastSeen).Seconds()
	visitorState.tokens = minFloat(rl.burst, visitorState.tokens+elapsedSec*rl.rps)
	visitorState.lastSeen = now
	if visitorState.tokens < 1 {
		return false
	}
	visitorState.tokens -= 1
	rl.cleanupLocked(now)
	return true
}

func (rl *clientRateLimiter) cleanupLocked(now time.Time) {
	if len(rl.visitors) < 2048 {
		return
	}
	for key, visitorState := range rl.visitors {
		if now.Sub(visitorState.lastSeen) > 10*time.Minute {
			delete(rl.visitors, key)
		}
	}
}

func queryClientKey(c *gin.Context) string {
	if ip := strings.TrimSpace(c.ClientIP()); ip != "" {
		return ip
	}
	return fmt.Sprintf("unknown:%s", c.Request.RemoteAddr)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
