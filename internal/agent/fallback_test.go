package agent

import "testing"

func TestFallbackControllerReturnsExpectedMode(t *testing.T) {
	controller := NewFallbackController()
	next, ok := controller.NextMode(ModeGraphMulti)
	if !ok {
		t.Fatal("expected graph_multi to have fallback")
	}
	if next != ModeMultiAgent {
		t.Fatalf("expected graph_multi to fallback to multi-agent, got %s", next)
	}
}
