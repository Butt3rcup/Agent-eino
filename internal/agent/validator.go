package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type DefaultValidator struct {
	allowedTools map[string]map[string]struct{}
}

func NewValidator() *DefaultValidator {
	return &DefaultValidator{
		allowedTools: map[string]map[string]struct{}{
			ModeReact:      toSet("hotword_search", "trend_analysis", "explain_hotword", "volcano_web_search"),
			ModeRAGAgent:   toSet("hotword_search", "trend_analysis", "explain_hotword", "volcano_web_search"),
			ModeMultiAgent: toSet("hotword_search", "trend_analysis", "explain_hotword", "volcano_web_search"),
			ModeGraphMulti: toSet("hotword_search", "trend_analysis", "explain_hotword", "volcano_web_search"),
		},
	}
}

func (v *DefaultValidator) AllowedTools(mode string) map[string]struct{} {
	if v == nil {
		return nil
	}
	return v.allowedTools[normalizeMode(mode)]
}

func (v *DefaultValidator) ValidateToolCall(mode, toolName, argumentsInJSON string) error {
	if v == nil {
		return nil
	}
	allowed := v.AllowedTools(mode)
	if len(allowed) > 0 {
		if _, ok := allowed[toolName]; !ok {
			return fmt.Errorf("tool %s is not allowed in mode %s", toolName, mode)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &payload); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	switch toolName {
	case "hotword_search", "trend_analysis", "explain_hotword":
		if !hasNonEmptyString(payload, "keyword") && !hasNonEmptyString(payload, "word") {
			return fmt.Errorf("tool %s requires keyword or word", toolName)
		}
	case "volcano_web_search":
		if !hasNonEmptyString(payload, "query") {
			return fmt.Errorf("tool %s requires query", toolName)
		}
	}
	return nil
}

func (v *DefaultValidator) ValidateToolResult(query, toolName, result string) error {
	_ = toolName
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return fmt.Errorf("tool result is empty")
	}
	if utf8.RuneCountInString(trimmed) < 8 {
		return fmt.Errorf("tool result is too short")
	}
	invalidSignals := []string{"failed", "error", "空结果", "暂无结果", "received empty response", "[]"}
	for _, signal := range invalidSignals {
		if strings.Contains(strings.ToLower(trimmed), strings.ToLower(signal)) {
			return fmt.Errorf("tool result is not useful")
		}
	}
	if query != "" && !sharesKeyword(query, trimmed) {
		return fmt.Errorf("tool result is weakly related to query")
	}
	return nil
}

func (v *DefaultValidator) ValidateAnswer(query, answer, evidence string) error {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return fmt.Errorf("answer is empty")
	}
	if utf8.RuneCountInString(trimmed) < 20 {
		return fmt.Errorf("answer is too short")
	}
	invalidSignals := []string{"我不确定", "无法确认", "可能", "也许", "根据最新官方公告", "已经证实"}
	for _, signal := range invalidSignals {
		if strings.Contains(trimmed, signal) && !strings.Contains(strings.TrimSpace(evidence), signal) {
			return fmt.Errorf("answer contains unsupported certainty or uncertainty statement")
		}
	}
	if query != "" && !sharesKeyword(query, trimmed) {
		return fmt.Errorf("answer is weakly related to query")
	}
	if strings.TrimSpace(evidence) != "" && !sharesKeyword(evidence, trimmed) {
		return fmt.Errorf("answer is not grounded in evidence")
	}
	return nil
}

func toSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func hasNonEmptyString(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	str, ok := value.(string)
	return ok && strings.TrimSpace(str) != ""
}

func sharesKeyword(source, target string) bool {
	for _, token := range tokenize(source) {
		if len([]rune(token)) < 2 {
			continue
		}
		if strings.Contains(target, token) {
			return true
		}
	}
	return false
}

func tokenize(text string) []string {
	text = strings.NewReplacer("，", " ", "。", " ", "？", " ", "?", " ", "、", " ", "：", " ", ":", " ", "（", " ", "）", " ", "\n", " ").Replace(strings.TrimSpace(text))
	parts := strings.Fields(text)
	if len(parts) > 0 {
		return parts
	}
	runes := []rune(text)
	result := make([]string, 0, len(runes))
	for idx := 0; idx < len(runes)-1; idx++ {
		result = append(result, string(runes[idx:idx+2]))
	}
	if len(result) == 0 && text != "" {
		result = append(result, text)
	}
	return result
}
