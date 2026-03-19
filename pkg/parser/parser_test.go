package parser

import (
	"strings"
	"testing"
)

func TestChunkTextPrefersSentenceBoundariesForChinese(t *testing.T) {
	parser := NewParser()
	text := "第一段介绍项目背景。第二段说明检索流程！第三段补充部署事项？最后一段总结。"

	chunks := parser.ChunkText(text, 18, 4)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "第一段介绍项目背景。") {
		t.Fatalf("expected first chunk to keep sentence boundary, got %q", chunks[0])
	}
	if !strings.Contains(chunks[1], "第二段说明检索流程！") {
		t.Fatalf("expected second chunk to keep Chinese sentence, got %q", chunks[1])
	}
}

func TestChunkTextAddsOverlapTail(t *testing.T) {
	parser := NewParser()
	text := "甲乙丙丁戊己庚辛壬癸。子丑寅卯辰巳午未申酉。"

	chunks := parser.ChunkText(text, 10, 3)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[1], "壬癸。") {
		t.Fatalf("expected overlap tail in next chunk, got %q", chunks[1])
	}
}
