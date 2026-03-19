package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"

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

	// 智能知识增强相关
	webSearchTool    tool.InvokableTool // 联网搜索工具
	enableAutoSearch bool               // 是否启用自动搜索
	threshold        float32            // 相似度阈值（L2距离）
	autoSave         bool               // 是否自动保存搜索结果
	autoSaveMinChars int                // 自动保存最小内容长度
	uploadDir        string             // 文件上传基础目录，从 config.Upload.Dir 读取
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
) *Service {
	return &Service{
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
	}
}

func (s *Service) Close() {
	if s.parser != nil {
		s.parser.Close()
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
		docs[i] = vectordb.Document{
			Content:  chunk,
			Metadata: metadata,
			Vector:   embeddings[i],
		}
	}

	if err := s.vectorDB.Insert(ctx, docs); err != nil {
		return fmt.Errorf("failed to insert documents: %w", err)
	}

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
	vector, err := s.embedding.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	results, err := s.vectorDB.Search(ctx, vector, s.topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	return filterSearchResults(query, results, s.maxDelta), nil
}

func (s *Service) BuildContext(ctx context.Context, query string) (string, error) {
	results, err := s.Search(ctx, query)
	if err != nil {
		return "", err
	}

	return s.buildContextFromResults(results), nil
}

// SmartRetrieve 智能检索：优先使用本地知识，相似度不足时触发联网搜索
// 返回值：(检索内容, 是否使用了联网搜索, 错误)
func (s *Service) SmartRetrieve(ctx context.Context, query string) (string, bool, error) {
	// 1. 本地向量搜索
	results, err := s.Search(ctx, query)
	if err != nil {
		return "", false, fmt.Errorf("local search failed: %w", err)
	}

	// 2. 检查相似度 (L2距离越小越相似)
	if len(results) > 0 && results[0].Score < s.threshold {
		// 本地知识充足，使用本地结果
		logger.Info("[SmartRetrieve] 本地知识充足，使用本地知识",
			zap.Float32("相似度", results[0].Score),
			zap.Float32("阈值", s.threshold))
		return s.buildContextFromResults(results), false, nil
	}

	// 3. 判断是否启用联网搜索
	if !s.enableAutoSearch {
		// 未启用自动搜索，返回最佳本地结果（如果有）
		if len(results) > 0 {
			return s.buildContextFromResults(results), false, nil
		}
		return "", false, fmt.Errorf("本地无相关知识且未启用联网搜索")
	}

	// 4. 本地知识不足，调用联网搜索工具
	logger.Info("[SmartRetrieve] 本地知识不足，触发联网搜索",
		zap.Float32("相似度", getFirstScore(results)),
		zap.Float32("阈值", s.threshold),
		zap.String("查询", query))

	searchArgs := map[string]interface{}{
		"query": query,
	}
	argsJSON, _ := json.Marshal(searchArgs)

	searchResult, err := s.webSearchTool.InvokableRun(ctx, string(argsJSON))
	if err != nil {
		return "", false, fmt.Errorf("联网搜索失败: %w", err)
	}

	// 5. 自动保存搜索结果到知识库
	if s.autoSave && searchResult != "" {
		if !shouldPersistKnowledge(s.autoSaveMinChars, query, searchResult) {
			logger.Info("[SmartRetrieve] 跳过保存联网结果",
				zap.String("查询", query),
				zap.Int("内容长度", utf8.RuneCountInString(strings.TrimSpace(searchResult))),
			)
		} else if err := s.saveSearchResult(ctx, query, searchResult); err != nil {
			logger.Warn("[SmartRetrieve] 保存搜索结果失败", zap.Error(err))
		} else {
			logger.Info("[SmartRetrieve] 已将搜索结果保存到知识库", zap.String("查询", query))
		}
	}

	return searchResult, true, nil
}

// saveSearchResult 将联网搜索结果保存到知识库
func (s *Service) saveSearchResult(ctx context.Context, query, answer string) error {
	now := time.Now().Format(time.RFC3339)
	normalizedQuery := strings.TrimSpace(query)
	normalizedAnswer := normalizeKnowledgeText(answer)
	answerHash := shortHash(normalizedQuery + "\n" + normalizedAnswer)

	metadataPayload := autoKnowledgeMetadata{
		SourceType:   "web_search",
		SourceName:   "volcano_web_search",
		Query:        normalizedQuery,
		AnswerHash:   answerHash,
		ReviewStatus: "pending_review",
		Traceability: "limited",
		SavedAt:      now,
	}
	metadataBytes, err := json.Marshal(metadataPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	content := fmt.Sprintf(`# 联网补充知识：%s

**问题**：%s

**回答**：
%s

---
*来源类型*：联网搜索补充
*来源工具*：volcano_web_search
*可追溯性*：limited
*审核状态*：pending_review
*时间*：%s
`, normalizedQuery, normalizedQuery, normalizedAnswer, now)

	// 使用配置的上传目录作为基础路径，避免硬编码相对路径
	baseDir := s.uploadDir
	if baseDir == "" {
		baseDir = "./uploads"
	}
	dir := filepath.Join(baseDir, "auto_knowledge")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := fmt.Sprintf("web_search_%s.md", answerHash)
	filePath := filepath.Join(dir, filename)
	if _, err := os.Stat(filePath); err == nil {
		logger.Info("[SmartRetrieve] 检测到重复联网结果，跳过重复入库",
			zap.String("查询", normalizedQuery),
			zap.String("hash", answerHash),
		)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := s.IndexDocument(ctx, content, string(metadataBytes)); err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	return nil
}

// getFirstScore 获取第一个结果的分数，如果没有结果返回无穷大
func getFirstScore(results []vectordb.SearchResult) float32 {
	if len(results) > 0 {
		return results[0].Score
	}
	return 999.0 // 表示无本地结果
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

		part := fmt.Sprintf(
			"[文档 %d] (相关度: %.4f)\n%s",
			i+1, result.Score, content,
		)
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
			contextParts = append(contextParts, fmt.Sprintf("[文档 1] (相关度: %.4f)\n%s", first.Score, content))
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
