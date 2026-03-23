package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type evalSummary struct {
	DatasetPath string                 `json:"dataset_path"`
	BaseURL     string                 `json:"base_url"`
	Modes       []string               `json:"modes"`
	Results     []evalResult           `json:"results"`
	ModeSummary map[string]modeSummary `json:"mode_summary"`
}

type evalResult struct {
	CaseID string `json:"case_id"`
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

type modeDetail struct {
	Mode                  string  `json:"mode"`
	Runs                  int     `json:"runs"`
	SuccessRate           float64 `json:"success_rate"`
	AvgScore              float64 `json:"avg_score"`
	AvgLatencyMs          float64 `json:"avg_latency_ms"`
	FallbackRate          float64 `json:"fallback_rate"`
	ToolFailureRate       float64 `json:"tool_failure_rate"`
	ValidationFailureRate float64 `json:"validation_failure_rate"`
}

type rankedMode struct {
	Rank int `json:"rank"`
	modeDetail
}

type report struct {
	DatasetPath string       `json:"dataset_path"`
	BaseURL     string       `json:"base_url"`
	TotalCases  int          `json:"total_cases"`
	TotalRuns   int          `json:"total_runs"`
	Modes       []modeDetail `json:"modes"`
	TopModes    []rankedMode `json:"top_modes"`
}

func main() {
	var (
		inputPath = flag.String("input", "tmp/agent-eval-report.json", "agent 评测结果 JSON 文件")
		output    = flag.String("output", "text", "输出格式：text/json")
		top       = flag.Int("top", 5, "Top 模式展示数量")
	)
	flag.Parse()

	summary, err := loadSummary(*inputPath)
	if err != nil {
		fail(fmt.Sprintf("读取 agent 评测结果失败: %v", err))
	}
	if len(summary.ModeSummary) == 0 {
		fail("agent 评测结果为空")
	}

	report := buildReport(summary, *top)
	switch strings.ToLower(strings.TrimSpace(*output)) {
	case "text":
		printReport(report)
	case "json":
		printReportJSON(report)
	default:
		fail(fmt.Sprintf("不支持的输出格式: %s", *output))
	}
}

func loadSummary(path string) (evalSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evalSummary{}, err
	}
	var summary evalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return evalSummary{}, err
	}
	return summary, nil
}

func buildReport(summary evalSummary, top int) report {
	if top <= 0 {
		top = 5
	}
	modeKeys := make([]string, 0, len(summary.ModeSummary))
	for mode := range summary.ModeSummary {
		modeKeys = append(modeKeys, mode)
	}
	sort.Strings(modeKeys)

	modes := make([]modeDetail, 0, len(modeKeys))
	for _, mode := range modeKeys {
		item := summary.ModeSummary[mode]
		modes = append(modes, modeDetail{
			Mode:                  mode,
			Runs:                  item.Runs,
			SuccessRate:           item.SuccessRate,
			AvgScore:              item.AvgScore,
			AvgLatencyMs:          item.AvgLatencyMs,
			FallbackRate:          item.FallbackRate,
			ToolFailureRate:       item.ToolFailureRate,
			ValidationFailureRate: item.ValidationFailure,
		})
	}

	ranked := make([]rankedMode, 0, len(modes))
	for _, item := range modes {
		ranked = append(ranked, rankedMode{modeDetail: item})
	}
	sort.Slice(ranked, func(i, j int) bool {
		left := ranked[i]
		right := ranked[j]
		if left.AvgScore == right.AvgScore {
			if left.SuccessRate == right.SuccessRate {
				if left.FallbackRate == right.FallbackRate {
					return left.AvgLatencyMs < right.AvgLatencyMs
				}
				return left.FallbackRate < right.FallbackRate
			}
			return left.SuccessRate > right.SuccessRate
		}
		return left.AvgScore > right.AvgScore
	})
	if len(ranked) > top {
		ranked = ranked[:top]
	}
	for idx := range ranked {
		ranked[idx].Rank = idx + 1
	}

	caseSet := make(map[string]struct{})
	for _, item := range summary.Results {
		if strings.TrimSpace(item.CaseID) == "" {
			continue
		}
		caseSet[item.CaseID] = struct{}{}
	}

	return report{
		DatasetPath: summary.DatasetPath,
		BaseURL:     summary.BaseURL,
		TotalCases:  len(caseSet),
		TotalRuns:   len(summary.Results),
		Modes:       modes,
		TopModes:    ranked,
	}
}

func printReport(report report) {
	fmt.Println("=== Agent Eval Report ===")
	fmt.Printf("Dataset: %s\n", report.DatasetPath)
	fmt.Printf("Base URL: %s\n", report.BaseURL)
	fmt.Printf("Cases: %d | Runs: %d\n", report.TotalCases, report.TotalRuns)

	fmt.Println("\n=== Mode Details ===")
	for _, item := range report.Modes {
		fmt.Printf("- %s | runs %d | score %.2f | success %.2f%% | avg %.2f ms | fallback %.2f%% | tool_fail %.2f | validation_fail %.2f\n",
			item.Mode,
			item.Runs,
			item.AvgScore,
			item.SuccessRate,
			item.AvgLatencyMs,
			item.FallbackRate,
			item.ToolFailureRate,
			item.ValidationFailureRate,
		)
	}

	fmt.Println("\n=== Top Modes ===")
	for _, item := range report.TopModes {
		fmt.Printf("%d. %s | score %.2f | success %.2f%% | fallback %.2f%% | avg %.2f ms\n",
			item.Rank,
			item.Mode,
			item.AvgScore,
			item.SuccessRate,
			item.FallbackRate,
			item.AvgLatencyMs,
		)
	}
}

func printReportJSON(report report) {
	data, err := json.Marshal(report)
	if err != nil {
		fail(fmt.Sprintf("输出 JSON 失败: %v", err))
	}
	fmt.Println(string(data))
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
