package rag

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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

	return results, nil
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
		if err := s.saveSearchResult(ctx, query, searchResult); err != nil {
			logger.Warn("[SmartRetrieve] 保存搜索结果失败", zap.Error(err))
		} else {
			logger.Info("[SmartRetrieve] 已将搜索结果保存到知识库", zap.String("查询", query))
		}
	}

	return searchResult, true, nil
}

// saveSearchResult 将联网搜索结果保存到知识库
func (s *Service) saveSearchResult(ctx context.Context, query, answer string) error {
	// 1. 生成文档内容
	content := fmt.Sprintf(`# %s

**问题**: %s

**回答**:
%s

---
*来源*: 联网搜索
*时间*: %s
`, query, query, answer, time.Now().Format(time.RFC3339))

	// 2. 生成文件名（使用MD5避免中文乱码）
	timestamp := time.Now().Format("20060102_150405")
	// 使用MD5哈希作为文件名，同时在文件内容中保留完整的中文查询
	hash := md5.Sum([]byte(query))
	hashStr := hex.EncodeToString(hash[:])[:16] // 取前16个字符
	filename := fmt.Sprintf("%s_%s.md", timestamp, hashStr)

	// 3. 确保目录存在
	dir := "uploads/auto_knowledge"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 4. 写入文件
	filePath := filepath.Join(dir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// 5. 索引到向量数据库
	metadata := fmt.Sprintf("source:web_search,query:%s,time:%s",
		query, time.Now().Format(time.RFC3339))
	if err := s.IndexDocument(ctx, content, metadata); err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	return nil
}

// sanitizeFilename 清理文件名中的非法字符
func sanitizeFilename(s string) string {
	replacements := map[string]string{
		"/": "_", "\\": "_", "?": "_", ":": "_",
		"*": "_", "\"": "_", "<": "_", ">": "_", "|": "_",
	}
	for old, new := range replacements {
		s = strings.ReplaceAll(s, old, new)
	}
	// 限制长度
	if len(s) > 50 {
		s = s[:50]
	}
	return s
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
	hash := md5.Sum([]byte(trimmed))
	return hex.EncodeToString(hash[:])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
