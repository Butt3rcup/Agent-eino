package rag

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"go-eino-agent/pkg/vectordb"
)

var structuredTokenPattern = regexp.MustCompile(`[A-Za-z0-9_:-]{4,}`)

func filterSearchResults(query string, results []vectordb.SearchResult, maxDelta float32) []vectordb.SearchResult {
	base := limitSearchResultsByScore(results, maxDelta)
	if len(base) == 0 {
		return []vectordb.SearchResult{}
	}

	structuredTerms := extractStructuredTerms(query)
	if len(structuredTerms) > 0 {
		matched := make([]vectordb.SearchResult, 0, len(base))
		for _, result := range base {
			combined := strings.ToLower(result.Content + "\n" + result.Metadata)
			if containsAnyTerm(combined, structuredTerms) {
				matched = append(matched, result)
			}
		}
		if len(matched) == 0 {
			return []vectordb.SearchResult{}
		}
		base = matched
	}

	chineseTerms := extractChineseTerms(query)
	if len(chineseTerms) == 0 {
		return base
	}

	type scoredResult struct {
		result   vectordb.SearchResult
		evidence int
	}

	withEvidence := make([]scoredResult, 0, len(base))
	for _, result := range base {
		combined := result.Content + "\n" + result.Metadata
		evidence := countContainedTerms(combined, chineseTerms)
		if evidence > 0 {
			withEvidence = append(withEvidence, scoredResult{result: result, evidence: evidence})
		}
	}
	if len(withEvidence) == 0 {
		return base
	}

	sort.SliceStable(withEvidence, func(i, j int) bool {
		if withEvidence[i].evidence == withEvidence[j].evidence {
			return withEvidence[i].result.Score < withEvidence[j].result.Score
		}
		return withEvidence[i].evidence > withEvidence[j].evidence
	})

	filtered := make([]vectordb.SearchResult, len(withEvidence))
	for index, item := range withEvidence {
		filtered[index] = item.result
	}
	return filtered
}

func limitSearchResultsByScore(results []vectordb.SearchResult, maxDelta float32) []vectordb.SearchResult {
	if len(results) == 0 {
		return []vectordb.SearchResult{}
	}
	bestScore := results[0].Score
	filtered := make([]vectordb.SearchResult, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if result.Score-bestScore > maxDelta {
			continue
		}
		content := strings.TrimSpace(result.Content)
		if content == "" {
			continue
		}
		key := contentHashKey(content)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, result)
	}
	return filtered
}

func extractStructuredTerms(query string) []string {
	matches := structuredTokenPattern.FindAllString(strings.ToLower(query), -1)
	return uniqueStrings(matches)
}

func extractChineseTerms(query string) []string {
	terms := make([]string, 0)
	for _, segment := range splitHanSegments(query) {
		runes := []rune(segment)
		for size := 4; size >= 2; size-- {
			if len(runes) < size {
				continue
			}
			for start := 0; start+size <= len(runes); start++ {
				term := string(runes[start : start+size])
				if isCommonChineseTerm(term) {
					continue
				}
				terms = append(terms, term)
			}
		}
	}
	return uniqueStrings(terms)
}

func splitHanSegments(query string) []string {
	segments := make([]string, 0)
	var current []rune
	flush := func() {
		if len(current) >= 2 {
			segments = append(segments, string(current))
		}
		current = current[:0]
	}
	for _, r := range query {
		if unicode.Is(unicode.Han, r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return segments
}

func isCommonChineseTerm(term string) bool {
	commonTerms := map[string]struct{}{
		"什么":  {},
		"一下":  {},
		"请解":  {},
		"解释":  {},
		"请问":  {},
		"含义":  {},
		"意思":  {},
		"网络":  {},
		"热词":  {},
		"一下意": {},
	}
	_, ok := commonTerms[term]
	return ok
}

func containsAnyTerm(content string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(content, term) {
			return true
		}
	}
	return false
}

func countContainedTerms(content string, terms []string) int {
	count := 0
	for _, term := range terms {
		if strings.Contains(content, term) {
			count++
		}
	}
	return count
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
