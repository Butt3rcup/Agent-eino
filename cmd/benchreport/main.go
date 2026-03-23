package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type loadSummary struct {
	Scenario     string         `json:"scenario"`
	BaseURL      string         `json:"base_url"`
	Mode         string         `json:"mode,omitempty"`
	Requests     int            `json:"requests"`
	Concurrency  int            `json:"concurrency"`
	Successes    int            `json:"successes"`
	Failures     int            `json:"failures"`
	SuccessRate  float64        `json:"success_rate"`
	LatencyAvgMs float64        `json:"latency_avg_ms"`
	LatencyP50Ms float64        `json:"latency_p50_ms"`
	LatencyP95Ms float64        `json:"latency_p95_ms"`
	LatencyMaxMs float64        `json:"latency_max_ms"`
	StatusCodes  map[string]int `json:"status_codes"`
	ErrorSamples []string       `json:"error_samples,omitempty"`
	GeneratedAt  string         `json:"generated_at"`
}

type rankedSummary struct {
	Rank         int     `json:"rank"`
	Label        string  `json:"label"`
	Scenario     string  `json:"scenario"`
	Mode         string  `json:"mode,omitempty"`
	SuccessRate  float64 `json:"success_rate"`
	LatencyAvgMs float64 `json:"latency_avg_ms"`
	LatencyP95Ms float64 `json:"latency_p95_ms"`
	LatencyMaxMs float64 `json:"latency_max_ms"`
	Failures     int     `json:"failures"`
	Requests     int     `json:"requests"`
}

type report struct {
	TotalScenarios     int             `json:"total_scenarios"`
	FastestByAvg       []rankedSummary `json:"fastest_by_avg"`
	SlowestByAvg       []rankedSummary `json:"slowest_by_avg"`
	SlowestByP95       []rankedSummary `json:"slowest_by_p95"`
	WorstByFailureRate []rankedSummary `json:"worst_by_failure_rate"`
}

func main() {
	var (
		inputPath = flag.String("input", "tmp/bench-report.ndjson", "压测结果文件路径，支持 JSON 数组或 JSON Lines")
		output    = flag.String("output", "text", "输出格式：text/json")
		top       = flag.Int("top", 5, "每个榜单展示前 N 项")
	)
	flag.Parse()

	summaries, err := loadSummaries(*inputPath)
	if err != nil {
		fail(fmt.Sprintf("读取压测结果失败: %v", err))
	}
	if len(summaries) == 0 {
		fail("压测结果为空")
	}

	report := buildReport(summaries, *top)
	switch strings.ToLower(strings.TrimSpace(*output)) {
	case "text":
		printReport(report)
	case "json":
		printReportJSON(report)
	default:
		fail(fmt.Sprintf("不支持的输出格式: %s", *output))
	}
}

func loadSummaries(path string) ([]loadSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var summaries []loadSummary
		if err := json.Unmarshal(data, &summaries); err != nil {
			return nil, err
		}
		return summaries, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	results := make([]loadSummary, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var summary loadSummary
		if err := json.Unmarshal([]byte(line), &summary); err != nil {
			return nil, err
		}
		results = append(results, summary)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func buildReport(summaries []loadSummary, top int) report {
	if top <= 0 {
		top = 5
	}
	fastest := rankedClone(summaries)
	sort.Slice(fastest, func(i, j int) bool {
		if fastest[i].LatencyAvgMs == fastest[j].LatencyAvgMs {
			return fastest[i].SuccessRate > fastest[j].SuccessRate
		}
		return fastest[i].LatencyAvgMs < fastest[j].LatencyAvgMs
	})

	slowest := rankedClone(summaries)
	sort.Slice(slowest, func(i, j int) bool {
		if slowest[i].LatencyAvgMs == slowest[j].LatencyAvgMs {
			return slowest[i].LatencyP95Ms > slowest[j].LatencyP95Ms
		}
		return slowest[i].LatencyAvgMs > slowest[j].LatencyAvgMs
	})

	slowestP95 := rankedClone(summaries)
	sort.Slice(slowestP95, func(i, j int) bool {
		if slowestP95[i].LatencyP95Ms == slowestP95[j].LatencyP95Ms {
			return slowestP95[i].LatencyAvgMs > slowestP95[j].LatencyAvgMs
		}
		return slowestP95[i].LatencyP95Ms > slowestP95[j].LatencyP95Ms
	})

	worstFailure := rankedClone(summaries)
	sort.Slice(worstFailure, func(i, j int) bool {
		leftFailure := 100 - worstFailure[i].SuccessRate
		rightFailure := 100 - worstFailure[j].SuccessRate
		if leftFailure == rightFailure {
			return worstFailure[i].LatencyAvgMs > worstFailure[j].LatencyAvgMs
		}
		return leftFailure > rightFailure
	})

	return report{
		TotalScenarios:     len(summaries),
		FastestByAvg:       topRanked(fastest, top),
		SlowestByAvg:       topRanked(slowest, top),
		SlowestByP95:       topRanked(slowestP95, top),
		WorstByFailureRate: topRanked(worstFailure, top),
	}
}

func rankedClone(summaries []loadSummary) []rankedSummary {
	results := make([]rankedSummary, 0, len(summaries))
	for _, summary := range summaries {
		results = append(results, rankedSummary{
			Label:        scenarioLabel(summary),
			Scenario:     summary.Scenario,
			Mode:         summary.Mode,
			SuccessRate:  summary.SuccessRate,
			LatencyAvgMs: summary.LatencyAvgMs,
			LatencyP95Ms: summary.LatencyP95Ms,
			LatencyMaxMs: summary.LatencyMaxMs,
			Failures:     summary.Failures,
			Requests:     summary.Requests,
		})
	}
	return results
}

func topRanked(values []rankedSummary, top int) []rankedSummary {
	if len(values) > top {
		values = values[:top]
	}
	for idx := range values {
		values[idx].Rank = idx + 1
	}
	return values
}

func scenarioLabel(summary loadSummary) string {
	if strings.TrimSpace(summary.Mode) != "" {
		return fmt.Sprintf("%s [%s]", summary.Scenario, summary.Mode)
	}
	return summary.Scenario
}

func printReport(report report) {
	fmt.Println("=== Bench Report ===")
	fmt.Printf("Total Scenarios: %d\n", report.TotalScenarios)
	printRanking("平均延迟最快", report.FastestByAvg)
	printRanking("平均延迟最慢", report.SlowestByAvg)
	printRanking("P95 延迟最慢", report.SlowestByP95)
	printRanking("失败率最高", report.WorstByFailureRate)
}

func printRanking(title string, values []rankedSummary) {
	fmt.Printf("\n=== %s ===\n", title)
	for _, item := range values {
		fmt.Printf("%d. %s | avg %.2f ms | p95 %.2f ms | success %.2f%% | failures %d/%d\n",
			item.Rank,
			item.Label,
			item.LatencyAvgMs,
			item.LatencyP95Ms,
			item.SuccessRate,
			item.Failures,
			item.Requests,
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
