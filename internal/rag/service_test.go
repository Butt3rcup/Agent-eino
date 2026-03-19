package rag

import (
	"strings"
	"testing"

	"go-eino-agent/pkg/vectordb"
)

func TestShouldAutoSaveSearchResultRejectsShortOrUncertainContent(t *testing.T) {
	if shouldPersistKnowledge(20, "热词", "太短了") {
		t.Fatal("expected short result to be rejected")
	}
	if shouldPersistKnowledge(20, "热词", "抱歉，我暂时无法确认这个问题的答案。") {
		t.Fatal("expected uncertain answer to be rejected")
	}
	if !shouldPersistKnowledge(20, "热词", "这是一个比较完整的联网补充说明，包含背景、语义和用法，所以应该允许入库。") {
		t.Fatal("expected sufficiently detailed answer to be accepted")
	}
}

func TestBuildContextFromResultsDeduplicatesAndHonorsLimits(t *testing.T) {
	service := &Service{
		maxDocs:  2,
		maxChars: 120,
		maxDelta: 0.5,
	}
	results := []vectordb.SearchResult{
		{Content: "第一条内容", Score: 0.1},
		{Content: "第一条内容", Score: 0.2},
		{Content: "第二条内容", Score: 0.3},
		{Content: "第三条内容", Score: 0.9},
	}

	context := service.buildContextFromResults(results)
	if strings.Count(context, "第一条内容") != 1 {
		t.Fatalf("expected duplicate content to be removed, got %q", context)
	}
	if !strings.Contains(context, "第二条内容") {
		t.Fatalf("expected second relevant document to remain, got %q", context)
	}
	if strings.Contains(context, "第三条内容") {
		t.Fatalf("expected low relevance document to be filtered, got %q", context)
	}
}
