package main

import "testing"

func TestRankQueryModesOrdersByLatencyThenFailureRate(t *testing.T) {
	metrics := map[string]queryModeMetric{
		"rag": {
			Requests:       10,
			Failures:       1,
			FailureRatePct: 10,
			AvgLatencyMs:   120,
		},
		"graph_multi": {
			Requests:       10,
			Failures:       2,
			FailureRatePct: 20,
			AvgLatencyMs:   300,
		},
		"react": {
			Requests:       10,
			Failures:       4,
			FailureRatePct: 40,
			AvgLatencyMs:   300,
		},
	}

	ranked := rankQueryModes(metrics)
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked modes, got %d", len(ranked))
	}
	if ranked[0].Mode != "react" {
		t.Fatalf("expected react to rank first, got %s", ranked[0].Mode)
	}
	if ranked[1].Mode != "graph_multi" {
		t.Fatalf("expected graph_multi to rank second, got %s", ranked[1].Mode)
	}
	if ranked[2].Mode != "rag" {
		t.Fatalf("expected rag to rank last, got %s", ranked[2].Mode)
	}
}
