package rag

import (
	"testing"
	"time"

	"go-eino-agent/pkg/vectordb"
)

func TestTTLCacheReturnsClonedCachedValue(t *testing.T) {
	cache := newTTLCache(4, time.Minute, cloneSearchResults)
	cache.Set("query", []vectordb.SearchResult{{Content: "a", Score: 1}}, 0)

	got, ok := cache.Get("query", 0)
	if !ok {
		t.Fatal("expected cache hit")
	}
	got[0].Content = "mutated"

	again, ok := cache.Get("query", 0)
	if !ok {
		t.Fatal("expected second cache hit")
	}
	if again[0].Content != "a" {
		t.Fatalf("expected cached value to be cloned, got %#v", again)
	}
}

func TestTTLCacheRespectsGeneration(t *testing.T) {
	cache := newTTLCache(4, time.Minute, cloneString)
	cache.Set("query", "context-a", 1)

	if _, ok := cache.Get("query", 0); ok {
		t.Fatal("expected generation mismatch to miss cache")
	}
	if got, ok := cache.Get("query", 1); !ok || got != "context-a" {
		t.Fatalf("expected generation match to hit cache, got %q %v", got, ok)
	}
}
