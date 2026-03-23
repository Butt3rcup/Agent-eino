package handler

import (
	"testing"
	"time"

	"go-eino-agent/config"
)

func TestBuildQueryModePoliciesUsesDefaultsAndOverrides(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			QueryDefaultTimeoutSec:     45,
			QueryDefaultRateLimitRPS:   8,
			QueryDefaultRateLimitBurst: 16,
			QueryModeTimeoutSec: map[string]int{
				"graph_multi": 90,
			},
			QueryModeRateLimitRPS: map[string]float64{
				"graph_multi": 2,
			},
			QueryModeRateLimitBurst: map[string]float64{
				"graph_multi": 4,
			},
		},
	}

	policies := buildQueryModePolicies(cfg)

	ragPolicy, ok := policies["rag"]
	if !ok {
		t.Fatal("expected rag policy to exist")
	}
	if ragPolicy.TimeoutSec != 45 {
		t.Fatalf("expected rag timeout 45 seconds, got %d", ragPolicy.TimeoutSec)
	}
	if ragPolicy.RateLimitRPS != 8 || ragPolicy.RateLimitBurst != 16 {
		t.Fatalf("expected rag rate limit 8/16, got %.2f/%.2f", ragPolicy.RateLimitRPS, ragPolicy.RateLimitBurst)
	}

	graphPolicy, ok := policies["graph_multi"]
	if !ok {
		t.Fatal("expected graph_multi policy to exist")
	}
	if graphPolicy.TimeoutSec != 90 {
		t.Fatalf("expected graph_multi timeout 90 seconds, got %d", graphPolicy.TimeoutSec)
	}
	if graphPolicy.RateLimitRPS != 2 || graphPolicy.RateLimitBurst != 4 {
		t.Fatalf("expected graph_multi rate limit 2/4, got %.2f/%.2f", graphPolicy.RateLimitRPS, graphPolicy.RateLimitBurst)
	}
}

func TestClientRateLimiterRejectsBurstOverflow(t *testing.T) {
	limiter := newClientRateLimiter(1, 1)
	if limiter == nil {
		t.Fatal("expected limiter to be created")
	}
	now := time.Now()
	if !limiter.Allow("127.0.0.1", now) {
		t.Fatal("expected first request to be allowed")
	}
	if limiter.Allow("127.0.0.1", now) {
		t.Fatal("expected immediate second request to be rejected")
	}
}
