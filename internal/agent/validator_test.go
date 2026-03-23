package agent

import "testing"

func TestValidatorRejectsDisallowedTool(t *testing.T) {
	validator := NewValidator()
	err := validator.ValidateToolCall(ModeRAGAgent, "unknown_tool", `{"query":"test"}`)
	if err == nil {
		t.Fatal("expected unknown tool to be rejected")
	}
}

func TestValidatorAcceptsValidWebSearchCall(t *testing.T) {
	validator := NewValidator()
	err := validator.ValidateToolCall(ModeRAGAgent, "volcano_web_search", `{"query":"最近的网络热词"}`)
	if err != nil {
		t.Fatalf("expected valid web search tool call, got %v", err)
	}
}

func TestValidatorRejectsUngroundedAnswer(t *testing.T) {
	validator := NewValidator()
	err := validator.ValidateAnswer("热词是什么意思", "根据最新官方公告，这个词已经完全过时了。", "这个词是最近流行的网络表达。")
	if err == nil {
		t.Fatal("expected unsupported answer to be rejected")
	}
}
