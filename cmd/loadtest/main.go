package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type runResult struct {
	Latency time.Duration
	Status  int
	Err     error
}

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

type uploadAcceptedResponse struct {
	TaskID string `json:"task_id"`
}

type uploadStatusResponse struct {
	State string `json:"state"`
	Error string `json:"error"`
}

func main() {
	var (
		scenario     = flag.String("scenario", "query", "压测场景：query/search/upload")
		baseURL      = flag.String("base-url", "http://localhost:8080", "服务地址")
		mode         = flag.String("mode", "rag", "query 场景使用的 mode")
		query        = flag.String("query", "最近有哪些网络热词？", "query/search 场景使用的问题")
		filePath     = flag.String("file", "", "upload 场景使用的文件路径")
		requests     = flag.Int("requests", 12, "总请求数")
		concurrency  = flag.Int("concurrency", 3, "并发数")
		timeout      = flag.Duration("timeout", 45*time.Second, "单次 HTTP 请求超时")
		pollInterval = flag.Duration("poll-interval", 800*time.Millisecond, "upload 状态轮询间隔")
		pollTimeout  = flag.Duration("poll-timeout", 2*time.Minute, "upload 单次任务轮询超时")
		output       = flag.String("output", "text", "输出格式：text/json")
		savePath     = flag.String("save", "", "将结果追加保存为 JSON Lines")
	)
	flag.Parse()

	if *requests <= 0 {
		fail("requests 必须大于 0")
	}
	if *concurrency <= 0 {
		fail("concurrency 必须大于 0")
	}

	base := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	selectedMode := strings.TrimSpace(*mode)
	selectedScenario := strings.ToLower(strings.TrimSpace(*scenario))
	client := &http.Client{Timeout: *timeout}

	var runner func() runResult
	switch selectedScenario {
	case "query":
		runner = func() runResult {
			return runQuery(client, base, strings.TrimSpace(*query), selectedMode)
		}
	case "search":
		runner = func() runResult {
			return runSearch(client, base, strings.TrimSpace(*query))
		}
	case "upload":
		if strings.TrimSpace(*filePath) == "" {
			fail("upload 场景必须提供 -file")
		}
		runner = func() runResult {
			return runUpload(client, base, *filePath, *pollInterval, *pollTimeout)
		}
	default:
		fail(fmt.Sprintf("不支持的场景: %s", *scenario))
	}

	results := runLoad(*requests, *concurrency, runner)
	summary := analyzeResults(selectedScenario, base, selectedMode, *requests, *concurrency, results)
	if err := saveSummary(*savePath, summary); err != nil {
		fail(fmt.Sprintf("保存压测结果失败: %v", err))
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

func runLoad(totalRequests, concurrency int, runner func() runResult) []runResult {
	jobs := make(chan struct{}, totalRequests)
	results := make(chan runResult, totalRequests)
	var wg sync.WaitGroup

	for workerID := 0; workerID < concurrency; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				results <- runner()
			}
		}()
	}

	for i := 0; i < totalRequests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	wg.Wait()
	close(results)

	collected := make([]runResult, 0, totalRequests)
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func runQuery(client *http.Client, baseURL, query, mode string) runResult {
	payload := map[string]string{"query": query, "mode": mode}
	return doJSONPost(client, baseURL+"/api/query", payload)
}

func runSearch(client *http.Client, baseURL, query string) runResult {
	payload := map[string]string{"query": query}
	return doJSONPost(client, baseURL+"/api/search", payload)
}

func doJSONPost(client *http.Client, url string, payload any) runResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return runResult{Err: err}
	}

	startedAt := time.Now()
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return runResult{Latency: time.Since(startedAt), Err: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return runResult{Latency: time.Since(startedAt), Status: resp.StatusCode}
}

func runUpload(client *http.Client, baseURL, filePath string, pollInterval, pollTimeout time.Duration) runResult {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return runResult{Err: err}
	}

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return runResult{Err: err}
	}
	if _, err := part.Write(fileData); err != nil {
		return runResult{Err: err}
	}
	if err := writer.Close(); err != nil {
		return runResult{Err: err}
	}

	startedAt := time.Now()
	resp, err := client.Post(baseURL+"/api/upload", writer.FormDataContentType(), &payload)
	if err != nil {
		return runResult{Latency: time.Since(startedAt), Err: err}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return runResult{Latency: time.Since(startedAt), Status: resp.StatusCode, Err: fmt.Errorf("upload failed: %s", strings.TrimSpace(string(respBody)))}
	}

	var accepted uploadAcceptedResponse
	if err := json.Unmarshal(respBody, &accepted); err != nil {
		return runResult{Latency: time.Since(startedAt), Status: resp.StatusCode, Err: fmt.Errorf("failed to parse upload response: %w", err)}
	}
	if strings.TrimSpace(accepted.TaskID) == "" {
		return runResult{Latency: time.Since(startedAt), Status: resp.StatusCode, Err: fmt.Errorf("upload response missing task_id")}
	}

	statusCode, err := waitForUploadTask(client, baseURL, accepted.TaskID, pollInterval, pollTimeout)
	return runResult{Latency: time.Since(startedAt), Status: statusCode, Err: err}
}

func waitForUploadTask(client *http.Client, baseURL, taskID string, pollInterval, pollTimeout time.Duration) (int, error) {
	deadline := time.Now().Add(pollTimeout)
	statusURL := fmt.Sprintf("%s/api/upload/%s", baseURL, taskID)
	for {
		if time.Now().After(deadline) {
			return http.StatusRequestTimeout, fmt.Errorf("upload task timeout: %s", taskID)
		}

		resp, err := client.Get(statusURL)
		if err != nil {
			return 0, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, fmt.Errorf("upload status failed: %s", strings.TrimSpace(string(body)))
		}

		var status uploadStatusResponse
		if err := json.Unmarshal(body, &status); err != nil {
			return resp.StatusCode, fmt.Errorf("failed to parse upload status: %w", err)
		}

		switch status.State {
		case "succeeded":
			return resp.StatusCode, nil
		case "failed":
			if strings.TrimSpace(status.Error) == "" {
				return resp.StatusCode, fmt.Errorf("upload task failed")
			}
			return resp.StatusCode, fmt.Errorf("upload task failed: %s", status.Error)
		}

		time.Sleep(pollInterval)
	}
}

func analyzeResults(scenario, baseURL, mode string, totalRequests, concurrency int, results []runResult) loadSummary {
	latencies := make([]time.Duration, 0, len(results))
	statusCount := make(map[int]int)
	successCount := 0
	failureCount := 0
	var totalLatency time.Duration
	var maxLatency time.Duration
	errorSamples := make([]string, 0, 3)

	for _, result := range results {
		latencies = append(latencies, result.Latency)
		totalLatency += result.Latency
		if result.Latency > maxLatency {
			maxLatency = result.Latency
		}
		if result.Status > 0 {
			statusCount[result.Status]++
		}
		if result.Err != nil || result.Status >= 400 || result.Status == 0 {
			failureCount++
			if result.Err != nil && len(errorSamples) < 3 {
				errorSamples = append(errorSamples, result.Err.Error())
			}
			continue
		}
		successCount++
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	avgLatency := time.Duration(0)
	if len(latencies) > 0 {
		avgLatency = totalLatency / time.Duration(len(latencies))
	}

	statusCodes := make(map[string]int, len(statusCount))
	for code, count := range statusCount {
		statusCodes[fmt.Sprintf("%d", code)] = count
	}

	summary := loadSummary{
		Scenario:     scenario,
		BaseURL:      baseURL,
		Requests:     totalRequests,
		Concurrency:  concurrency,
		Successes:    successCount,
		Failures:     failureCount,
		SuccessRate:  percentage(successCount, len(results)),
		LatencyAvgMs: durationMs(avgLatency),
		LatencyP50Ms: durationMs(percentileLatency(latencies, 0.50)),
		LatencyP95Ms: durationMs(percentileLatency(latencies, 0.95)),
		LatencyMaxMs: durationMs(maxLatency),
		StatusCodes:  statusCodes,
		ErrorSamples: errorSamples,
		GeneratedAt:  time.Now().Format(time.RFC3339),
	}
	if scenario == "query" && mode != "" {
		summary.Mode = mode
	}
	return summary
}

func printSummary(summary loadSummary) {
	fmt.Printf("=== Load Test Summary ===\n")
	fmt.Printf("Scenario: %s\n", summary.Scenario)
	fmt.Printf("Base URL: %s\n", summary.BaseURL)
	if summary.Mode != "" {
		fmt.Printf("Mode: %s\n", summary.Mode)
	}
	fmt.Printf("Requests: %d\n", summary.Requests)
	fmt.Printf("Concurrency: %d\n", summary.Concurrency)
	fmt.Printf("Success: %d\n", summary.Successes)
	fmt.Printf("Failures: %d\n", summary.Failures)
	fmt.Printf("Success Rate: %.2f%%\n", summary.SuccessRate)
	fmt.Printf("Latency Avg: %.2f ms\n", summary.LatencyAvgMs)
	fmt.Printf("Latency P50: %.2f ms\n", summary.LatencyP50Ms)
	fmt.Printf("Latency P95: %.2f ms\n", summary.LatencyP95Ms)
	fmt.Printf("Latency Max: %.2f ms\n", summary.LatencyMaxMs)
	fmt.Printf("Status Codes: %s\n", formatStatusCodeSummary(summary.StatusCodes))
	if len(summary.ErrorSamples) > 0 {
		fmt.Printf("Error Samples:\n")
		for _, sample := range summary.ErrorSamples {
			fmt.Printf("- %s\n", sample)
		}
	}
}

func printSummaryJSON(summary loadSummary) {
	data, err := json.Marshal(summary)
	if err != nil {
		fail(fmt.Sprintf("输出 JSON 失败: %v", err))
	}
	fmt.Println(string(data))
}

func saveSummary(path string, summary loadSummary) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func percentileLatency(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * percentile)
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func formatStatusCodeSummary(statusCount map[string]int) string {
	if len(statusCount) == 0 {
		return "none"
	}
	codes := make([]string, 0, len(statusCount))
	for code := range statusCount {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%s=%d", code, statusCount[code]))
	}
	return strings.Join(parts, ", ")
}

func percentage(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func durationMs(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
