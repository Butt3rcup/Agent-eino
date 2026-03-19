package rag

import "testing"

func TestShouldPersistKnowledge(t *testing.T) {
	if shouldPersistKnowledge(20, "热词", "太短") {
		t.Fatal("expected short answer to be rejected")
	}
	if shouldPersistKnowledge(20, "热词", "抱歉，我暂时无法确认这个问题的答案。") {
		t.Fatal("expected uncertain answer to be rejected")
	}
	if !shouldPersistKnowledge(20, "热词", "这是一个比较完整的联网补充说明，包含背景、语义和用法，所以应该允许入库。") {
		t.Fatal("expected detailed answer to be accepted")
	}
}

func TestNormalizeKnowledgeText(t *testing.T) {
	got := normalizeKnowledgeText("  第一行\n\n 第二行\t ")
	if got != "第一行 第二行" {
		t.Fatalf("unexpected normalized text: %q", got)
	}
}
