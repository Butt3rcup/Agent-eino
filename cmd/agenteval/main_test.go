package main

import "testing"

func TestNormalizeAnswer(t *testing.T) {
	answer := "ℹ️ mode: rag\n\n第一行\n🔡 tokens: 120\n 第二行  "
	got := normalizeAnswer(answer)
	want := "第一行\n第二行"
	if got != want {
		t.Fatalf("normalizeAnswer() = %q, want %q", got, want)
	}
}

func TestDiffMetrics(t *testing.T) {
	before := metricsSnapshot{
		AgentModes: map[string]agentModeMetric{
			"rag_agent": {Requests: 2, Fallbacks: 0, ToolCalls: 3, ToolFailures: 1, ValidationFailures: 0},
		},
	}
	after := metricsSnapshot{
		AgentModes: map[string]agentModeMetric{
			"rag_agent": {Requests: 3, Fallbacks: 1, ToolCalls: 5, ToolFailures: 1, ValidationFailures: 1},
		},
	}

	mode, delta := diffMetrics(before, after)
	if mode != "rag_agent" {
		t.Fatalf("diffMetrics() mode = %q, want %q", mode, "rag_agent")
	}
	if delta.Requests != 1 || delta.Fallbacks != 1 || delta.ToolCalls != 2 || delta.ToolFailures != 0 || delta.ValidationFailures != 1 {
		t.Fatalf("diffMetrics() delta = %+v, unexpected", delta)
	}
}

func TestCountMatches(t *testing.T) {
	answer := "这个梗的含义很明确，起源也常见于短视频场景。"
	count, matched := countMatches(answer, []string{"含义", "起源", "场景", "热度"})
	if count != 3 {
		t.Fatalf("countMatches() count = %d, want 3", count)
	}
	want := []string{"含义", "起源", "场景"}
	for idx, item := range want {
		if matched[idx] != item {
			t.Fatalf("countMatches() matched[%d] = %q, want %q", idx, matched[idx], item)
		}
	}
}

func TestCalculateScore(t *testing.T) {
	metrics := agentModeMetric{ToolFailures: 1, ValidationFailures: 1}
	got := calculateScore(true, 2, 4, 1, true, metrics)
	want := 45
	if got != want {
		t.Fatalf("calculateScore() = %d, want %d", got, want)
	}
}
