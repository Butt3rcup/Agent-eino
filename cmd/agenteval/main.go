package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type evalCase struct {
	ID                string   `json:"id"`
	Query             string   `json:"query"`
	ExpectedKeywords  []string `json:"expected_keywords"`
	ForbiddenKeywords []string `json:"forbidden_keywords"`
	PreferredModes    []string `json:"preferred_modes"`
}

type metricsSnapshot struct {
	QueryModes map[string]queryModeMetric `json:"query_modes"`
	AgentModes map[string]agentModeMetric `json:"agent_modes"`
}

type queryModeMetric struct {
	Requests uint64 `json:"requests"`
}

type agentModeMetric struct {
	Requests           uint64  `json:"requests"`
	Fallbacks          uint64  `json:"fallbacks"`
	ToolCalls          uint64  `json:"tool_calls"`
	ToolFailures       uint64  `json:"tool_failures"`
	ValidationFailures uint64  `json:"validation_failures"`
	FallbackRatePct    float64 `json:"fallback_rate_pct"`
	ToolFailureRatePct float64 `json:"tool_failure_rate_pct"`
}

type evalResult struct {
	CaseID             string   `json:"case_id"`
	Mode               string   `json:"mode"`
	ResolvedMode       string   `json:"resolved_mode,omitempty"`
	LatencyMs          float64  `json:"latency_ms"`
	Success            bool     `json:"success"`
	Score              int      `json:"score"`
	ExpectedHits       int      `json:"expected_hits"`
	ForbiddenHits      int      `json:"forbidden_hits"`
	FallbackTriggered  bool     `json:"fallback_triggered"`
	ToolCalls          uint64   `json:"tool_calls"`
	ToolFailures       uint64   `json:"tool_failures"`
	ValidationFailures uint64   `json:"validation_failures"`
	ErrorMessage       string   `json:"error_message,omitempty"`
	Answer             string   `json:"answer"`
	PreferredModeHit   bool     `json:"preferred_mode_hit"`
	MatchedKeywords    []string `json:"matched_keywords,omitempty"`
	MatchedForbidden   []string `json:"matched_forbidden,omitempty"`
	GeneratedAt        string   `json:"generated_at"`
}

type evalSummary struct {
	DatasetPath string                 `json:"dataset_path"`
	BaseURL     string                 `json:"base_url"`
	Modes       []string               `json:"modes"`
	Results     []evalResult           `json:"results"`
	ModeSummary map[string]modeSummary `json:"mode_summary"`
}

type modeSummary struct {
	Runs              int     `json:"runs"`
	SuccessRate       float64 `json:"success_rate"`
	AvgScore          float64 `json:"avg_score"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	FallbackRate      float64 `json:"fallback_rate"`
	ToolFailureRate   float64 `json:"tool_failure_rate"`
	ValidationFailure float64 `json:"validation_failure_rate"`
}

func main() {
	var (
		baseURL = flag.String("base-url", "http://localhost:8080", "服务地址")
		dataset = flag.String("dataset", "testdata/agent_eval_cases.json", "golden dataset 路径")
		modes   = flag.String("modes", "rag,rag_agent,multi-agent,graph_multi", "要评测的 mode 列表")
		timeout = flag.Duration("timeout", 90*time.Second, "单次评测超时")
		output  = flag.String("output", "text", "输出格式：text/json")
		save    = flag.String("save", "", "保存 JSON 结果文件")
	)
	flag.Parse()

	client := &http.Client{Timeout: *timeout}
	base := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	selectedModes := parseModes(*modes)
	if len(selectedModes) == 0 {
		fail("至少需要一个评测 mode")
	}
	cases, err := loadCases(*dataset)
	if err != nil {
		fail(fmt.Sprintf("读取评测数据失败: %v", err))
	}

	results := make([]evalResult, 0, len(cases)*len(selectedModes))
	for _, currentCase := range cases {
		for _, mode := range selectedModes {
			result := runEvaluation(client, base, currentCase, mode)
			results = append(results, result)
		}
	}
	summary := buildSummary(*dataset, base, selectedModes, results)
	if err := saveSummary(*save, summary); err != nil {
		fail(fmt.Sprintf("保存评测结果失败: %v", err))
	}
	switch strings.ToLower(strings.TrimSpace(*output)) {
	case "text":
		printSummary(summary)
	case "json":
		printSummaryJSON(summary)
	default:
		fail(fmt.Sprintf("不支持的输出格式: %s", *output))
	}
}

func loadCases(path string) ([]evalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []evalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func runEvaluation(client *http.Client, baseURL string, currentCase evalCase, mode string) evalResult {
	before, _ := fetchMetrics(client, baseURL)
	startedAt := time.Now()
	answer, err := queryAgent(client, baseURL, currentCase.Query, mode)
	latency := time.Since(startedAt)
	after, _ := fetchMetrics(client, baseURL)

	resolvedMode, agentDelta := diffMetrics(before, after)
	expectedHits, matchedKeywords := countMatches(answer, currentCase.ExpectedKeywords)
	forbiddenHits, matchedForbidden := countMatches(answer, currentCase.ForbiddenKeywords)
	preferredHit := containsString(currentCase.PreferredModes, resolvedMode)
	if resolvedMode == "" {
		preferredHit = containsString(currentCase.PreferredModes, mode)
	}
	score := calculateScore(err == nil, expectedHits, len(currentCase.ExpectedKeywords), forbiddenHits, preferredHit, agentDelta)

	result := evalResult{
		CaseID:             currentCase.ID,
		Mode:               mode,
		ResolvedMode:       resolvedMode,
		LatencyMs:          toMs(latency),
		Success:            err == nil,
		Score:              score,
		ExpectedHits:       expectedHits,
		ForbiddenHits:      forbiddenHits,
		FallbackTriggered:  agentDelta.Fallbacks > 0,
		ToolCalls:          agentDelta.ToolCalls,
		ToolFailures:       agentDelta.ToolFailures,
		ValidationFailures: agentDelta.ValidationFailures,
		Answer:             answer,
		PreferredModeHit:   preferredHit,
		MatchedKeywords:    matchedKeywords,
		MatchedForbidden:   matchedForbidden,
		GeneratedAt:        time.Now().Format(time.RFC3339),
	}
	if err != nil {
		result.ErrorMessage = err.Error()
	}
	return result
}

func fetchMetrics(client *http.Client, baseURL string) (metricsSnapshot, error) {
	resp, err := client.Get(baseURL + "/api/metrics")
	if err != nil {
		return metricsSnapshot{}, err
	}
	defer resp.Body.Close()
	var snapshot metricsSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return metricsSnapshot{}, err
	}
	return snapshot, nil
}

func queryAgent(client *http.Client, baseURL, query, mode string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"query": query, "mode": mode})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/query", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("query failed: %s", strings.TrimSpace(string(body)))
	}
	answer, err := parseSSEAnswer(resp.Body)
	if err != nil {
		return answer, err
	}
	return normalizeAnswer(answer), nil
}

func parseSSEAnswer(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	currentEvent := ""
	parts := make([]string, 0, 16)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var message map[string]any
		if err := json.Unmarshal([]byte(payload), &message); err != nil {
			continue
		}
		switch currentEvent {
		case "message":
			if content, ok := message["content"].(string); ok && strings.TrimSpace(content) != "" {
				parts = append(parts, content)
			}
		case "error":
			if msg, ok := message["message"].(string); ok {
				return strings.Join(parts, ""), errors.New(msg)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return strings.Join(parts, ""), err
	}
	return strings.Join(parts, ""), nil
}

func normalizeAnswer(answer string) string {
	lines := strings.Split(answer, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "ℹ️") || strings.HasPrefix(trimmed, "🔡") {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, "\n")
}

func diffMetrics(before, after metricsSnapshot) (string, agentModeMetric) {
	resolvedMode := ""
	delta := agentModeMetric{}
	for mode, afterMetric := range after.AgentModes {
		beforeMetric := before.AgentModes[mode]
		if afterMetric.Requests > beforeMetric.Requests {
			resolvedMode = mode
			delta = agentModeMetric{
				Requests:           afterMetric.Requests - beforeMetric.Requests,
				Fallbacks:          afterMetric.Fallbacks - beforeMetric.Fallbacks,
				ToolCalls:          afterMetric.ToolCalls - beforeMetric.ToolCalls,
				ToolFailures:       afterMetric.ToolFailures - beforeMetric.ToolFailures,
				ValidationFailures: afterMetric.ValidationFailures - beforeMetric.ValidationFailures,
			}
			break
		}
	}
	return resolvedMode, delta
}

func countMatches(answer string, keywords []string) (int, []string) {
	matched := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if strings.Contains(answer, keyword) {
			matched = append(matched, keyword)
		}
	}
	return len(matched), matched
}

func calculateScore(success bool, expectedHits, expectedTotal, forbiddenHits int, preferredHit bool, metrics agentModeMetric) int {
	score := 0
	if success {
		score += 40
	}
	if expectedTotal > 0 {
		score += int(float64(expectedHits) / float64(expectedTotal) * 30)
	}
	if preferredHit {
		score += 10
	}
	score -= forbiddenHits * 10
	score -= int(metrics.ToolFailures) * 5
	score -= int(metrics.ValidationFailures) * 5
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func buildSummary(datasetPath, baseURL string, modes []string, results []evalResult) evalSummary {
	modeSummaryMap := make(map[string]modeSummary, len(modes))
	for _, mode := range modes {
		var runs int
		var successes int
		var totalScore int
		var totalLatency float64
		var fallbacks int
		var toolFailures int
		var validationFailures int
		for _, result := range results {
			if result.Mode != mode {
				continue
			}
			runs++
			if result.Success {
				successes++
			}
			totalScore += result.Score
			totalLatency += result.LatencyMs
			if result.FallbackTriggered {
				fallbacks++
			}
			toolFailures += int(result.ToolFailures)
			validationFailures += int(result.ValidationFailures)
		}
		if runs == 0 {
			continue
		}
		modeSummaryMap[mode] = modeSummary{
			Runs:              runs,
			SuccessRate:       percentage(successes, runs),
			AvgScore:          float64(totalScore) / float64(runs),
			AvgLatencyMs:      totalLatency / float64(runs),
			FallbackRate:      percentage(fallbacks, runs),
			ToolFailureRate:   float64(toolFailures) / float64(runs),
			ValidationFailure: float64(validationFailures) / float64(runs),
		}
	}
	return evalSummary{DatasetPath: datasetPath, BaseURL: baseURL, Modes: modes, Results: results, ModeSummary: modeSummaryMap}
}

func printSummary(summary evalSummary) {
	fmt.Println("=== Agent Eval Summary ===")
	fmt.Printf("Dataset: %s\n", summary.DatasetPath)
	fmt.Printf("Base URL: %s\n", summary.BaseURL)
	modeKeys := make([]string, 0, len(summary.ModeSummary))
	for mode := range summary.ModeSummary {
		modeKeys = append(modeKeys, mode)
	}
	sort.Strings(modeKeys)
	for _, mode := range modeKeys {
		item := summary.ModeSummary[mode]
		fmt.Printf("- %s | runs %d | success %.2f%% | score %.2f | avg %.2f ms | fallback %.2f%% | tool_fail %.2f | validation_fail %.2f\n",
			mode,
			item.Runs,
			item.SuccessRate,
			item.AvgScore,
			item.AvgLatencyMs,
			item.FallbackRate,
			item.ToolFailureRate,
			item.ValidationFailure,
		)
	}
	best := bestModes(summary.ModeSummary)
	if len(best) > 0 {
		fmt.Println("\n=== Best Modes by Score ===")
		for idx, mode := range best {
			item := summary.ModeSummary[mode]
			fmt.Printf("%d. %s | score %.2f | success %.2f%% | avg %.2f ms\n", idx+1, mode, item.AvgScore, item.SuccessRate, item.AvgLatencyMs)
		}
	}
}

func bestModes(values map[string]modeSummary) []string {
	modes := make([]string, 0, len(values))
	for mode := range values {
		modes = append(modes, mode)
	}
	sort.Slice(modes, func(i, j int) bool {
		left := values[modes[i]]
		right := values[modes[j]]
		if left.AvgScore == right.AvgScore {
			if left.SuccessRate == right.SuccessRate {
				return left.AvgLatencyMs < right.AvgLatencyMs
			}
			return left.SuccessRate > right.SuccessRate
		}
		return left.AvgScore > right.AvgScore
	})
	if len(modes) > 5 {
		return modes[:5]
	}
	return modes
}

func printSummaryJSON(summary evalSummary) {
	data, err := json.Marshal(summary)
	if err != nil {
		fail(fmt.Sprintf("输出 JSON 失败: %v", err))
	}
	fmt.Println(string(data))
}

func saveSummary(path string, summary evalSummary) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func parseModes(raw string) []string {
	parts := strings.Split(raw, ",")
	results := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		results = append(results, part)
	}
	return results
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func toMs(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func percentage(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
