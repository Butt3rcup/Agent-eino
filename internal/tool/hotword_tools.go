package tool

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"

	"go-eino-agent/pkg/embedding"
	"go-eino-agent/pkg/logger"
	"go-eino-agent/pkg/vectordb"
)

// SearchCache 搜索结果缓存
type SearchCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
	ttl     time.Duration
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Results   []HotwordResult
	Timestamp time.Time
}

// NewSearchCache 创建搜索缓存
func NewSearchCache(maxSize int, ttl time.Duration) *SearchCache {
	return &SearchCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get 获取缓存结果
func (c *SearchCache) Get(key string) ([]HotwordResult, bool) {
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// 检查是否过期（在持锁外判断，避免读锁升写锁）
	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Results, true
}

// Set 设置缓存
func (c *SearchCache) Set(key string, results []HotwordResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.removeExpiredLocked()

	// 如果超过最大容量，删除最旧的条目
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = &CacheEntry{
		Results:   results,
		Timestamp: time.Now(),
	}
}

// evictOldest 驱逐最旧的缓存条目
func (c *SearchCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.entries {
		if oldestKey == "" || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *SearchCache) removeExpiredLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.Timestamp) > c.ttl {
			delete(c.entries, key)
		}
	}
}

// generateCacheKey 生成缓存键
func generateCacheKey(keyword string) string {
	hash := md5.Sum([]byte(strings.ToLower(keyword)))
	return hex.EncodeToString(hash[:])
}

// HotwordSearchTool 网络热词搜索工具（先查数据库，无结果则百度搜索）
type HotwordSearchTool struct {
	vectorDB  *vectordb.MilvusClient
	embedding *embedding.Service
	cache     *SearchCache
}

type HotwordSearchInput struct {
	Keyword string `json:"keyword" jsonschema:"description=要搜索的网络热词"`
}

type HotwordSearchOutput struct {
	Results []HotwordResult `json:"results"`
}

type HotwordResult struct {
	Word        string `json:"word"`
	Explanation string `json:"explanation"`
	Example     string `json:"example"`
	Popularity  int    `json:"popularity"`
}

func NewHotwordSearchTool(vectorDB *vectordb.MilvusClient, embeddingSvc *embedding.Service) *HotwordSearchTool {
	return &HotwordSearchTool{
		vectorDB:  vectorDB,
		embedding: embeddingSvc,
		cache:     NewSearchCache(100, 30*time.Minute), // 缓存100条，30分钟过期
	}
}

func (t *HotwordSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "hotword_search",
		Desc: "搜索网络热词的含义、用法和流行度",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"keyword": {
				Type: schema.String,
				Desc: "要搜索的网络热词",
			},
		}),
	}, nil
}

func (t *HotwordSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input HotwordSearchInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	logger.Info("[HotwordSearch] 开始搜索热词", zap.String("keyword", input.Keyword))

	// 1. 检查缓存
	cacheKey := generateCacheKey(input.Keyword)
	if cachedResults, found := t.cache.Get(cacheKey); found {
		logger.Info("[HotwordSearch] 从缓存找到结果", zap.String("keyword", input.Keyword))
		output := HotwordSearchOutput{Results: cachedResults}
		data, _ := json.Marshal(output)
		return string(data), nil
	}

	// 2. 查询向量数据库
	dbResults, err := t.searchInDatabase(ctx, input.Keyword)
	if err != nil {
		logger.Warn("[HotwordSearch] 数据库查询失败，将使用百度搜索", zap.Error(err))
	} else if len(dbResults) > 0 {
		logger.Info("[HotwordSearch] 从数据库找到结果", zap.Int("count", len(dbResults)))
		// 缓存结果
		t.cache.Set(cacheKey, dbResults)
		output := HotwordSearchOutput{Results: dbResults}
		data, _ := json.Marshal(output)
		return string(data), nil
	}

	// 3. 数据库无结果，使用百度搜索
	logger.Info("[HotwordSearch] 数据库无结果，使用百度搜索", zap.String("keyword", input.Keyword))
	baiduResults, err := t.searchFromBaidu(ctx, input.Keyword)
	if err != nil {
		logger.Error("[HotwordSearch] 百度搜索失败", zap.Error(err))
		// 返回空结果而不是错误
		return `{"results":[]}`, nil
	}

	// 缓存百度搜索结果
	if len(baiduResults) > 0 {
		t.cache.Set(cacheKey, baiduResults)
	}

	output := HotwordSearchOutput{Results: baiduResults}
	data, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(data), nil
}

// searchInDatabase 从向量数据库搜索热词
func (t *HotwordSearchTool) searchInDatabase(ctx context.Context, keyword string) ([]HotwordResult, error) {
	if t.vectorDB == nil {
		return nil, fmt.Errorf("vector database not initialized")
	}

	if t.embedding == nil {
		return nil, fmt.Errorf("embedding service not initialized")
	}

	// 1. 将关键词向量化
	vector, err := t.embedding.Embed(ctx, keyword)
	if err != nil {
		logger.Warn("[HotwordSearch] 向量化失败", zap.Error(err))
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}

	// 2. 在 Milvus 中搜索相似文档
	searchResults, err := t.vectorDB.Search(ctx, vector, 3) // 获取前3个最相似的结果
	if err != nil {
		logger.Warn("[HotwordSearch] Milvus 搜索失败", zap.Error(err))
		return nil, fmt.Errorf("failed to search in Milvus: %w", err)
	}

	// 3. 如果没有结果或相似度太低，返回空
	if len(searchResults) == 0 {
		logger.Info("[HotwordSearch] Milvus 未找到相关结果")
		return nil, nil
	}

	// 4. 过滤低相似度结果（L2距离阈值：1.5）
	const similarityThreshold = 1.5
	var validResults []vectordb.SearchResult
	for _, result := range searchResults {
		if result.Score < similarityThreshold {
			validResults = append(validResults, result)
		}
	}

	if len(validResults) == 0 {
		logger.Info("[HotwordSearch] 所有结果相似度过低",
			zap.Float32("threshold", similarityThreshold))
		return nil, nil
	}

	// 5. 将搜索结果转换为 HotwordResult
	results := make([]HotwordResult, 0, len(validResults))
	for _, result := range validResults {
		// 解析内容提取热词信息
		hotwordResult := t.parseContentToHotwordResult(result.Content, keyword, result.Score)
		if hotwordResult != nil {
			results = append(results, *hotwordResult)
		}
	}

	logger.Info("[HotwordSearch] Milvus 搜索成功",
		zap.Int("totalResults", len(searchResults)),
		zap.Int("validResults", len(results)))

	return results, nil
}

// parseContentToHotwordResult 从向量数据库内容解析热词信息
func (t *HotwordSearchTool) parseContentToHotwordResult(content, keyword string, score float32) *HotwordResult {
	// 内容格式示例：
	// # YYDS
	// **解释**: 永远的神...
	// **示例**: ...

	lines := strings.Split(content, "\n")
	result := &HotwordResult{
		Word:       keyword,
		Popularity: int((1.0 - score/2.0) * 100), // 根据相似度估算流行度
	}

	for i, line := range lines {
		line = strings.TrimSpace(line)

		// 提取标题作为热词
		if strings.HasPrefix(line, "# ") {
			result.Word = strings.TrimPrefix(line, "# ")
		}

		// 提取解释
		if strings.Contains(line, "**解释**") || strings.Contains(line, "**含义**") ||
			strings.Contains(line, "**问题**") {
			// 提取冒号后的内容
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				result.Explanation = strings.TrimSpace(parts[1])
			} else if i+1 < len(lines) {
				// 如果解释在下一行
				result.Explanation = strings.TrimSpace(lines[i+1])
			}
		}

		// 提取示例
		if strings.Contains(line, "**示例**") || strings.Contains(line, "**例子**") ||
			strings.Contains(line, "**用法**") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				result.Example = strings.TrimSpace(parts[1])
			} else if i+1 < len(lines) {
				result.Example = strings.TrimSpace(lines[i+1])
			}
		}
	}

	// 如果解释为空，使用部分内容
	if result.Explanation == "" && len(content) > 0 {
		// 取前200个字符作为解释
		if len(content) > 200 {
			result.Explanation = content[:200] + "..."
		} else {
			result.Explanation = content
		}
	}

	// 如果示例为空，生成一个默认示例
	if result.Example == "" {
		result.Example = fmt.Sprintf("参考文档中关于 '%s' 的说明", keyword)
	}

	return result
}

// searchFromBaidu 从百度搜索热词信息
func (t *HotwordSearchTool) searchFromBaidu(ctx context.Context, keyword string) ([]HotwordResult, error) {
	// 构建百度搜索URL
	query := fmt.Sprintf("%s 是什么意思 网络用语", keyword)
	searchURL := fmt.Sprintf("https://www.baidu.com/s?wd=%s", url.QueryEscape(query))

	logger.Info("[HotwordSearch] 百度搜索", zap.String("url", searchURL))

	// 发起HTTP请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置User-Agent避免被拦截
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from baidu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("baidu returned status: %d", resp.StatusCode)
	}

	// 使用 goquery 解析 HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// 提取搜索结果摘要
	explanation := t.extractExplanationWithGoquery(doc, keyword)

	if explanation == "" {
		explanation = fmt.Sprintf("'%s' 是一个网络流行用语。（来源：百度搜索）", keyword)
	}

	result := HotwordResult{
		Word:        keyword,
		Explanation: explanation,
		Example:     fmt.Sprintf("搜索结果来自百度：%s", keyword),
		Popularity:  70, // 默认流行度
	}

	logger.Info("[HotwordSearch] 百度搜索成功", zap.String("keyword", keyword))

	return []HotwordResult{result}, nil
}

// extractExplanationWithGoquery 使用 goquery 从HTML中提取解释
func (t *HotwordSearchTool) extractExplanationWithGoquery(doc *goquery.Document, keyword string) string {
	var explanation string

	// 策略1: 查找百度百科摘要（c-abstract 类）
	doc.Find(".c-abstract").Each(func(i int, s *goquery.Selection) {
		if i == 0 { // 只取第一个结果
			text := s.Text()
			text = strings.TrimSpace(text)
			if len(text) > 20 && strings.Contains(text, keyword) {
				explanation = text
				return
			}
		}
	})

	if explanation != "" {
		return t.cleanText(explanation)
	}

	// 策略2: 查找内容摘要（content-right 类）
	doc.Find(".content-right_8Zs40").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			text := s.Text()
			text = strings.TrimSpace(text)
			if len(text) > 20 {
				explanation = text
				return
			}
		}
	})

	if explanation != "" {
		return t.cleanText(explanation)
	}

	// 策略3: 查找通用搜索结果描述
	doc.Find(".c-span18.c-span-last").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			text := s.Text()
			text = strings.TrimSpace(text)
			if len(text) > 20 && strings.Contains(text, keyword) {
				explanation = text
				return
			}
		}
	})

	if explanation != "" {
		return t.cleanText(explanation)
	}

	// 策略4: 查找任何包含关键词的段落
	doc.Find("p, div").Each(func(i int, s *goquery.Selection) {
		if explanation != "" {
			return
		}
		text := s.Text()
		text = strings.TrimSpace(text)
		if len(text) > 50 && len(text) < 500 &&
			strings.Contains(text, keyword) &&
			(strings.Contains(text, "意思") || strings.Contains(text, "含义") ||
				strings.Contains(text, "指") || strings.Contains(text, "表示")) {
			explanation = text
		}
	})

	return t.cleanText(explanation)
}

// cleanText 清理文本内容
func (t *HotwordSearchTool) cleanText(text string) string {
	text = strings.Join(strings.Fields(text), " ")

	// 限制长度
	if len(text) > 300 {
		text = text[:300] + "..."
	}

	return text
}

// TrendAnalysisTool 热词趋势分析工具
type TrendAnalysisTool struct{}

type TrendAnalysisInput struct {
	Keyword   string `json:"keyword" jsonschema:"description=要分析的热词"`
	TimeRange string `json:"time_range" jsonschema:"description=时间范围，如：7d, 30d, 90d"`
}

type TrendAnalysisOutput struct {
	Keyword    string      `json:"keyword"`
	Trend      string      `json:"trend"`
	DataPoints []DataPoint `json:"data_points"`
}

type DataPoint struct {
	Date       string `json:"date"`
	Popularity int    `json:"popularity"`
}

func NewTrendAnalysisTool() *TrendAnalysisTool {
	return &TrendAnalysisTool{}
}

func (t *TrendAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "trend_analysis",
		Desc: "分析网络热词的流行趋势",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"keyword": {
				Type: schema.String,
				Desc: "要分析的热词",
			},
			"time_range": {
				Type: schema.String,
				Desc: "时间范围，如：7d, 30d, 90d",
			},
		}),
	}, nil
}

func (t *TrendAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input TrendAnalysisInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.TimeRange == "" {
		input.TimeRange = "7d"
	}

	// 模拟趋势数据
	output := t.mockTrendAnalysis(input.Keyword, input.TimeRange)

	data, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(data), nil
}

func (t *TrendAnalysisTool) mockTrendAnalysis(keyword, timeRange string) TrendAnalysisOutput {
	days := 7
	if strings.HasPrefix(timeRange, "30") {
		days = 30
	} else if strings.HasPrefix(timeRange, "90") {
		days = 90
	}

	dataPoints := make([]DataPoint, days)
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -days+i+1)
		popularity := 50 + (i * 5) // 模拟上升趋势
		dataPoints[i] = DataPoint{
			Date:       date.Format("2006-01-02"),
			Popularity: popularity,
		}
	}

	trend := "上升"
	if dataPoints[len(dataPoints)-1].Popularity < dataPoints[0].Popularity {
		trend = "下降"
	}

	return TrendAnalysisOutput{
		Keyword:    keyword,
		Trend:      trend,
		DataPoints: dataPoints,
	}
}

// ExplanationTool 热词解释工具
type ExplanationTool struct{}

type ExplanationInput struct {
	Keyword string `json:"keyword" jsonschema:"description=需要解释的热词"`
}

type ExplanationOutput struct {
	Keyword     string   `json:"keyword"`
	Explanation string   `json:"explanation"`
	Origin      string   `json:"origin"`
	Usage       []string `json:"usage"`
	Related     []string `json:"related"`
}

func NewExplanationTool() *ExplanationTool {
	return &ExplanationTool{}
}

func (t *ExplanationTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "explain_hotword",
		Desc: "详细解释网络热词的含义、起源和用法",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"keyword": {
				Type: schema.String,
				Desc: "需要解释的热词",
			},
		}),
	}, nil
}

func (t *ExplanationTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input ExplanationInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	output := t.mockExplanation(input.Keyword)

	data, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(data), nil
}

func (t *ExplanationTool) mockExplanation(keyword string) ExplanationOutput {
	return ExplanationOutput{
		Keyword:     keyword,
		Explanation: fmt.Sprintf("'%s' 是一个流行的网络用语，广泛用于社交媒体和日常交流中", keyword),
		Origin:      "起源于网络社区，通过短视频平台迅速传播",
		Usage: []string{
			fmt.Sprintf("在表达赞美时使用：这个真%s！", keyword),
			fmt.Sprintf("在评论中使用：%s，必须支持", keyword),
		},
		Related: []string{"网络流行语", "年轻人用语", "社交媒体"},
	}
}
