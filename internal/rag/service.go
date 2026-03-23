package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"go-eino-agent/pkg/embedding"
	"go-eino-agent/pkg/logger"
	"go-eino-agent/pkg/parser"
	"go-eino-agent/pkg/vectordb"
)

type Service struct {
	vectorDB  *vectordb.MilvusClient
	embedding *embedding.Service
	parser    *parser.Parser
	chunkSize int
	overlap   int
	topK      int
	maxDocs   int
	maxChars  int
	maxDelta  float32

	webSearchTool    tool.InvokableTool
	enableAutoSearch bool
	threshold        float32
	autoSave         bool
	autoSaveMinChars int
	uploadDir        string
	persistQueue     *persistQueue
	indexGeneration  atomic.Uint64
	embeddingCache   *ttlCache[[]float32]
	searchCache      *ttlCache[[]vectordb.SearchResult]
	contextCache     *ttlCache[string]
	embeddingGroup   singleflight.Group
	searchGroup      singleflight.Group
	contextGroup     singleflight.Group
	webSearchGroup   singleflight.Group
}

type autoKnowledgeMetadata struct {
	SourceType   string `json:"source_type"`
	SourceName   string `json:"source_name"`
	Query        string `json:"query"`
	AnswerHash   string `json:"answer_hash"`
	ReviewStatus string `json:"review_status"`
	Traceability string `json:"traceability"`
	SavedAt      string `json:"saved_at"`
}

func NewService(
	vectorDB *vectordb.MilvusClient,
	embeddingSvc *embedding.Service,
	chunkSize, overlap, topK int,
	maxContextDocs, maxContextChars int,
	maxScoreDelta float32,
	webSearchTool tool.InvokableTool,
	enableAutoSearch bool,
	threshold float32,
	autoSave bool,
	autoSaveMinChars int,
	uploadDir string,
	asyncKnowledgePersist bool,
	persistQueueSize int,
	queryCacheSize int,
	queryCacheTTL time.Duration,
) *Service {
	service := &Service{
		vectorDB:         vectorDB,
		embedding:        embeddingSvc,
		parser:           parser.NewParser(),
		chunkSize:        chunkSize,
		overlap:          overlap,
		topK:             topK,
		maxDocs:          maxContextDocs,
		maxChars:         maxContextChars,
		maxDelta:         maxScoreDelta,
		webSearchTool:    webSearchTool,
		enableAutoSearch: enableAutoSearch,
		threshold:        threshold,
		autoSave:         autoSave,
		autoSaveMinChars: autoSaveMinChars,
		uploadDir:        uploadDir,
		embeddingCache:   newTTLCache(queryCacheSize, queryCacheTTL, cloneFloat32Slice),
		searchCache:      newTTLCache(queryCacheSize, queryCacheTTL, cloneSearchResults),
		contextCache:     newTTLCache(queryCacheSize, queryCacheTTL, cloneString),
	}
	if autoSave && asyncKnowledgePersist {
		service.persistQueue = newPersistQueue(persistQueueSize, 30*time.Second, service.saveSearchResult)
	}
	return service
}

func (s *Service) Close() {
	if s.persistQueue != nil {
		s.persistQueue.Close()
	}
	if s.parser != nil {
		s.parser.Close()
	}
}

func (s *Service) QueueStats() *PersistQueueStats {
	if s.persistQueue == nil {
		return &PersistQueueStats{Enabled: false}
	}
	stats := s.persistQueue.Stats()
	return &stats
}

func (s *Service) CacheStats() *QueryCacheStats {
	return &QueryCacheStats{
		Embedding: s.metricOrDisabled(s.embeddingCache),
		Search:    s.metricOrDisabled(s.searchCache),
		Context:   s.metricOrDisabled(s.contextCache),
	}
}

func (s *Service) IndexDocument(ctx context.Context, content, metadata string) error {
	chunks := s.parser.ChunkText(content, s.chunkSize, s.overlap)
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks generated from content")
	}

	embeddings, err := s.embedding.EmbedBatch(ctx, chunks)
	if err != nil {
		return fmt.Errorf("failed to embed chunks: %w", err)
	}

	docs := make([]vectordb.Document, len(chunks))
	for i, chunk := range chunks {
		docs[i] = vectordb.Document{Content: chunk, Metadata: metadata, Vector: embeddings[i]}
	}

	if err := s.vectorDB.Insert(ctx, docs); err != nil {
		return fmt.Errorf("failed to insert documents: %w", err)
	}
	s.indexGeneration.Add(1)
	return nil
}

func (s *Service) IndexFile(ctx context.Context, filePath, metadata string) error {
	content, err := s.parser.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}
	return s.IndexDocument(ctx, content, metadata)
}

func (s *Service) Search(ctx context.Context, query string) ([]vectordb.SearchResult, error) {
	generation := s.indexGeneration.Load()
	if cached, ok := s.searchCache.Get(query, generation); ok {
		return cached, nil
	}

	value, err, _ := s.searchGroup.Do(s.requestKey("search", query, generation), func() (interface{}, error) {
		if cached, ok := s.searchCache.Get(query, generation); ok {
			return cloneSearchResults(cached), nil
		}

		vector, err := s.getQueryEmbedding(ctx, query, generation)
		if err != nil {
			return nil, fmt.Errorf("failed to embed query: %w", err)
		}

		results, err := s.vectorDB.Search(ctx, vector, s.topK)
		if err != nil {
			return nil, fmt.Errorf("failed to search: %w", err)
		}
		filtered := filterSearchResults(query, results, s.maxDelta)
		s.searchCache.Set(query, filtered, generation)
		return cloneSearchResults(filtered), nil
	})
	if err != nil {
		return nil, err
	}
	results, ok := value.([]vectordb.SearchResult)
	if !ok {
		return nil, fmt.Errorf("unexpected search result type: %T", value)
	}
	return cloneSearchResults(results), nil
}

func (s *Service) BuildContext(ctx context.Context, query string) (string, error) {
	generation := s.indexGeneration.Load()
	if cached, ok := s.contextCache.Get(query, generation); ok {
		return cached, nil
	}

	value, err, _ := s.contextGroup.Do(s.requestKey("context", query, generation), func() (interface{}, error) {
		if cached, ok := s.contextCache.Get(query, generation); ok {
			return cloneString(cached), nil
		}

		results, err := s.Search(ctx, query)
		if err != nil {
			return nil, err
		}
		contextText := s.buildContextFromResults(results)
		s.contextCache.Set(query, contextText, generation)
		return contextText, nil
	})
	if err != nil {
		return "", err
	}
	contextText, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("unexpected context result type: %T", value)
	}
	return contextText, nil
}

func (s *Service) SmartRetrieve(ctx context.Context, query string) (string, bool, error) {
	results, err := s.Search(ctx, query)
	if err != nil {
		return "", false, fmt.Errorf("local search failed: %w", err)
	}

	if len(results) > 0 && results[0].Score < s.threshold {
		logger.Info("[SmartRetrieve] use local context",
			zap.Float32("score", results[0].Score),
			zap.Float32("threshold", s.threshold),
		)
		return s.buildContextFromResults(results), false, nil
	}

	if !s.enableAutoSearch || s.webSearchTool == nil {
		if len(results) > 0 {
			return s.buildContextFromResults(results), false, nil
		}
		return "", false, fmt.Errorf("no relevant local knowledge and web search is unavailable")
	}

	logger.Info("[SmartRetrieve] fallback to web search",
		zap.Float32("score", getFirstScore(results)),
		zap.Float32("threshold", s.threshold),
		zap.String("query", query),
	)

	generation := s.indexGeneration.Load()
	value, err, _ := s.webSearchGroup.Do(s.requestKey("web", query, generation), func() (interface{}, error) {
		argsJSON, err := json.Marshal(map[string]any{"query": query})
		if err != nil {
			return nil, fmt.Errorf("failed to build search arguments: %w", err)
		}

		searchResult, err := s.webSearchTool.InvokableRun(ctx, string(argsJSON))
		if err != nil {
			return nil, fmt.Errorf("web search failed: %w", err)
		}

		if s.autoSave && searchResult != "" {
			s.persistSearchResult(query, searchResult)
		}

		return searchResult, nil
	})
	if err != nil {
		return "", false, err
	}
	searchResult, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("unexpected web search result type: %T", value)
	}
	return searchResult, true, nil
}

func (s *Service) persistSearchResult(query, answer string) {
	if !shouldPersistKnowledge(s.autoSaveMinChars, query, answer) {
		logger.Info("[SmartRetrieve] skip persistence",
			zap.String("query", query),
			zap.Int("content_length", utf8.RuneCountInString(strings.TrimSpace(answer))),
		)
		return
	}

	if s.persistQueue != nil {
		if !s.persistQueue.Enqueue(query, answer) {
			logger.Warn("[SmartRetrieve] persistence queue is full", zap.String("query", query))
			return
		}
		logger.Info("[SmartRetrieve] queued search result persistence", zap.String("query", query))
		return
	}

	if err := s.saveSearchResult(context.Background(), query, answer); err != nil {
		logger.Warn("[SmartRetrieve] save search result failed", zap.Error(err))
		return
	}
	logger.Info("[SmartRetrieve] search result persisted", zap.String("query", query))
}

func (s *Service) saveSearchResult(ctx context.Context, query, answer string) error {
	now := time.Now().Format(time.RFC3339)
	normalizedQuery := strings.TrimSpace(query)
	normalizedAnswer := normalizeKnowledgeText(answer)
	answerHash := shortHash(normalizedQuery + "\n" + normalizedAnswer)

	metadataBytes, err := json.Marshal(autoKnowledgeMetadata{
		SourceType:   "web_search",
		SourceName:   "volcano_web_search",
		Query:        normalizedQuery,
		AnswerHash:   answerHash,
		ReviewStatus: "pending_review",
		Traceability: "limited",
		SavedAt:      now,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	content := fmt.Sprintf("# 联网补充知识：%s\n\n**问题**：%s\n\n**回答**：\n%s\n\n---\n*来源类型*：联网搜索补充\n*来源工具*：volcano_web_search\n*可追溯性*：limited\n*审核状态*：pending_review\n*时间*：%s\n", normalizedQuery, normalizedQuery, normalizedAnswer, now)

	baseDir := s.uploadDir
	if baseDir == "" {
		baseDir = "./uploads"
	}
	dir := filepath.Join(baseDir, "auto_knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := fmt.Sprintf("web_search_%s.md", answerHash)
	filePath := filepath.Join(dir, filename)
	if _, err := os.Stat(filePath); err == nil {
		logger.Info("[SmartRetrieve] duplicate web search result skipped",
			zap.String("query", normalizedQuery),
			zap.String("hash", answerHash),
		)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := s.IndexDocument(ctx, content, string(metadataBytes)); err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}
	return nil
}

func getFirstScore(results []vectordb.SearchResult) float32 {
	if len(results) > 0 {
		return results[0].Score
	}
	return 999.0
}

func (s *Service) buildContextFromResults(results []vectordb.SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	sortedResults := make([]vectordb.SearchResult, len(results))
	copy(sortedResults, results)
	sort.Slice(sortedResults, func(i, j int) bool { return sortedResults[i].Score < sortedResults[j].Score })

	bestScore := sortedResults[0].Score
	contextParts := make([]string, 0, minInt(s.maxDocs, len(sortedResults)))
	seenContent := make(map[string]struct{}, len(sortedResults))
	currentChars := 0

	for i, result := range sortedResults {
		if len(contextParts) >= s.maxDocs {
			break
		}
		if result.Score-bestScore > s.maxDelta {
			continue
		}
		content := strings.TrimSpace(result.Content)
		if content == "" {
			continue
		}
		key := contentHashKey(content)
		if _, exists := seenContent[key]; exists {
			continue
		}

		part := fmt.Sprintf("[文档 %d] (相关度 %.4f)\n%s", i+1, result.Score, content)
		partChars := utf8.RuneCountInString(part)
		if currentChars+partChars > s.maxChars {
			break
		}
		seenContent[key] = struct{}{}
		contextParts = append(contextParts, part)
		currentChars += partChars
	}

	if len(contextParts) == 0 {
		first := sortedResults[0]
		content := strings.TrimSpace(first.Content)
		if content != "" {
			contextParts = append(contextParts, fmt.Sprintf("[文档 1] (相关度 %.4f)\n%s", first.Score, content))
		}
	}
	return strings.Join(contextParts, "\n\n")
}

func contentHashKey(content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) > 512 {
		trimmed = trimmed[:512]
	}
	return shortHash(trimmed)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) getQueryEmbedding(ctx context.Context, query string, generation uint64) ([]float32, error) {
	if cached, ok := s.embeddingCache.Get(query, generation); ok {
		return cached, nil
	}

	value, err, _ := s.embeddingGroup.Do(s.requestKey("embedding", query, generation), func() (interface{}, error) {
		if cached, ok := s.embeddingCache.Get(query, generation); ok {
			return cloneFloat32Slice(cached), nil
		}
		vector, err := s.embedding.Embed(ctx, query)
		if err != nil {
			return nil, err
		}
		s.embeddingCache.Set(query, vector, generation)
		return cloneFloat32Slice(vector), nil
	})
	if err != nil {
		return nil, err
	}
	vector, ok := value.([]float32)
	if !ok {
		return nil, fmt.Errorf("unexpected embedding result type: %T", value)
	}
	return cloneFloat32Slice(vector), nil
}

func (s *Service) metricOrDisabled(cache interface{ Stats() QueryCacheMetric }) QueryCacheMetric {
	if cache == nil {
		return QueryCacheMetric{Enabled: false}
	}
	return cache.Stats()
}

func (s *Service) requestKey(prefix, query string, generation uint64) string {
	return fmt.Sprintf("%s:%d:%s", prefix, generation, strings.TrimSpace(query))
}

func cloneFloat32Slice(values []float32) []float32 {
	if values == nil {
		return nil
	}
	cloned := make([]float32, len(values))
	copy(cloned, values)
	return cloned
}

func cloneSearchResults(values []vectordb.SearchResult) []vectordb.SearchResult {
	if values == nil {
		return nil
	}
	cloned := make([]vectordb.SearchResult, len(values))
	copy(cloned, values)
	return cloned
}

func cloneString(value string) string {
	return value
}
