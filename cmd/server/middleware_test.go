package main

import (
	"testing"
	"time"
)

func TestRateLimiterBlocksBurstExhaustion(t *testing.T) {
	rl := newRateLimiter(1, 2)
	now := time.Now()

	if !rl.allow("127.0.0.1", now) {
		t.Fatal("expected first request to pass")
	}
	if !rl.allow("127.0.0.1", now) {
		t.Fatal("expected second request to pass within burst")
	}
	if rl.allow("127.0.0.1", now) {
		t.Fatal("expected third request to be blocked after burst exhaustion")
	}
	if !rl.allow("127.0.0.1", now.Add(time.Second)) {
		t.Fatal("expected token refill after one second")
	}
}
