package rag

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

func shouldPersistKnowledge(minChars int, query, answer string) bool {
	normalizedQuery := strings.TrimSpace(query)
	normalizedAnswer := normalizeKnowledgeText(answer)
	if normalizedQuery == "" || normalizedAnswer == "" {
		return false
	}
	if utf8.RuneCountInString(normalizedAnswer) < minChars {
		return false
	}
	blockedPhrases := []string{
		"抱歉",
		"无法确认",
		"无法确定",
		"无法获取",
		"未找到",
		"没有足够",
		"不确定",
		"作为 ai",
		"作为一个 ai",
		"建议查询官网",
	}
	lowerAnswer := strings.ToLower(normalizedAnswer)
	for _, phrase := range blockedPhrases {
		if strings.Contains(lowerAnswer, phrase) {
			return false
		}
	}
	return true
}

func shortHash(content string) string {
	hash := md5.Sum([]byte(content))
	return hex.EncodeToString(hash[:])[:16]
}

func normalizeKnowledgeText(content string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
}
