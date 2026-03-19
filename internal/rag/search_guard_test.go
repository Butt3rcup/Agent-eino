package rag

import (
	"testing"

	"go-eino-agent/pkg/vectordb"
)

func TestFilterSearchResultsDropsStructuredNoiseWithoutLiteralEvidence(t *testing.T) {
	results := []vectordb.SearchResult{
		{Content: "YYDS 是永远的神", Metadata: "filename:test.md", Score: 0.9},
		{Content: "网络热词合集", Metadata: "filename:hotwords.md", Score: 1.0},
	}

	filtered := filterSearchResults("qzxwplm_nonexistent_token_20260318", results, 1.0)
	if len(filtered) != 0 {
		t.Fatalf("expected nonsense structured query to be filtered out, got %#v", filtered)
	}
}

func TestFilterSearchResultsKeepsStructuredLiteralMatches(t *testing.T) {
	results := []vectordb.SearchResult{
		{Content: "YYDS 是永远的神", Metadata: "filename:yyds.md", Score: 0.8},
		{Content: "emo 是情绪低落", Metadata: "filename:emo.md", Score: 0.85},
	}

	filtered := filterSearchResults("YYDS 是什么意思", results, 1.0)
	if len(filtered) != 1 {
		t.Fatalf("expected only literal token match to remain, got %#v", filtered)
	}
	if filtered[0].Metadata != "filename:yyds.md" {
		t.Fatalf("expected YYDS result to remain, got %#v", filtered[0])
	}
}

func TestFilterSearchResultsPreservesChineseSemanticResults(t *testing.T) {
	results := []vectordb.SearchResult{
		{Content: "这个词用来表达高度赞美和崇拜。", Metadata: "filename:praise.md", Score: 0.7},
		{Content: "另一个无关结果", Metadata: "filename:other.md", Score: 1.9},
	}

	filtered := filterSearchResults("夸人的网络用语有哪些", results, 1.0)
	if len(filtered) != 1 {
		t.Fatalf("expected semantic Chinese result to remain, got %#v", filtered)
	}
	if filtered[0].Metadata != "filename:praise.md" {
		t.Fatalf("expected praise result to remain, got %#v", filtered[0])
	}
}
