package agent

import (
	"context"
	"testing"
)

func TestRouterResolvesComprehensiveQueryToGraphMulti(t *testing.T) {
	router := NewRouter()
	route, err := router.Resolve(context.Background(), "这个热词是什么意思、为什么火、怎么用、会不会过时？请详细分析并总结。", "")
	if err != nil {
		t.Fatalf("expected router to succeed, got %v", err)
	}
	if route.ResolvedMode != ModeGraphMulti {
		t.Fatalf("expected graph_multi, got %s", route.ResolvedMode)
	}
	if !route.NeedPlanning {
		t.Fatal("expected route to require planning")
	}
}

func TestRouterRespectsRequestedMode(t *testing.T) {
	router := NewRouter()
	route, err := router.Resolve(context.Background(), "给我解释一下这个梗", ModeReact)
	if err != nil {
		t.Fatalf("expected router to succeed, got %v", err)
	}
	if route.ResolvedMode != ModeReact {
		t.Fatalf("expected requested mode react, got %s", route.ResolvedMode)
	}
}
