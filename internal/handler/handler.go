package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"go-eino-agent/config"
	"go-eino-agent/internal/agent"
	"go-eino-agent/internal/graph"
	"go-eino-agent/internal/rag"
	hotwordTool "go-eino-agent/internal/tool"
	"go-eino-agent/pkg/embedding"
	"go-eino-agent/pkg/logger"
	"go-eino-agent/pkg/vectordb"
)

type Handler struct {
	cfg          *config.Config
	ragService   *rag.Service
	chatModel    model.ChatModel
	milvusClient *vectordb.MilvusClient

	reactAgent *agent.ReActAgent
	multiAgent *agent.MultiAgentSystem
	ragAgent   *agent.RAGAgent
	ragGraph   *graph.RAGGraph
	multiGraph *graph.MultiStageGraph
}

var toolCallDescriptionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`用户.*调用.*函数`),
	regexp.MustCompile(`调用.*函数.*获取`),
	regexp.MustCompile(`使用.*工具.*获取`),
	regexp.MustCompile(`想了解.*调用.*函数`),
}

func NewHandler(cfg *config.Config) (*Handler, error) {
	embeddingSvc, err := embedding.NewService(
		cfg.Ark.APIKey,
		cfg.Ark.BaseURL,
		cfg.Ark.Embedder,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding service: %w", err)
	}

	milvusClient, err := vectordb.NewMilvusClient(
		cfg.Milvus.URI,
		cfg.Milvus.Token,
		cfg.RAG.EmbeddingDim,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create milvus client: %w", err)
	}

	if err := milvusClient.CreateCollection(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	// 创建共享 ChatModel（所有 Agent / Graph 复用同一实例）
	var tcChatModel model.ToolCallingChatModel
	var baseChatModel model.ChatModel
	if cfg.Ollama.BaseURL != "" && cfg.Ollama.Model != "" {
		// 使用本地 Ollama 模型
		ollamaModel, err := ollama.NewChatModel(context.Background(), &ollama.ChatModelConfig{
			BaseURL: cfg.Ollama.BaseURL,
			Model:   cfg.Ollama.Model,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create ollama chat model: %w", err)
		}
		tcChatModel = ollamaModel
		baseChatModel = ollamaModel
		logger.Info("Using local Ollama model", zap.String("baseURL", cfg.Ollama.BaseURL), zap.String("model", cfg.Ollama.Model))
	} else {
		// 回退使用火山引擎 Ark 模型
		arkModel, err := arkComponent.NewChatModel(context.Background(), &arkComponent.ChatModelConfig{
			APIKey:  cfg.Ark.APIKey,
			BaseURL: cfg.Ark.BaseURL,
			Model:   cfg.Ark.Model,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create ark chat model: %w", err)
		}
		tcChatModel = arkModel
		baseChatModel = arkModel
		logger.Info("Using Ark model", zap.String("baseURL", cfg.Ark.BaseURL), zap.String("model", cfg.Ark.Model))
	}

	// 创建火山引擎联网搜索工具（RAG Service 和 Agent 共用同一实例）
	volcanoSearchTool, err := hotwordTool.NewVolcanoWebSearchTool(
		cfg.Ark.APIKey,
		cfg.Ark.BaseURL,
		cfg.Ark.Model,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create volcano web search tool: %w", err)
	}

	ragService := rag.NewService(
		milvusClient,
		embeddingSvc,
		cfg.RAG.ChunkSize,
		cfg.RAG.ChunkOverlap,
		cfg.RAG.TopK,
		cfg.RAG.MaxContextDocs,
		cfg.RAG.MaxContextChars,
		cfg.RAG.MaxScoreDelta,
		volcanoSearchTool,
		cfg.RAG.EnableAutoSearch,
		cfg.RAG.SimilarityThreshold,
		cfg.RAG.AutoSaveSearchResult,
		cfg.Upload.Dir, // 传入上传目录，供自动知识保存使用
	)

	toolset := []tool.BaseTool{
		hotwordTool.NewHotwordSearchTool(milvusClient, embeddingSvc),
		volcanoSearchTool, // 复用已创建的搜索工具
		hotwordTool.NewTrendAnalysisTool(),
		hotwordTool.NewExplanationTool(),
	}

	reactAgent, err := agent.NewReActAgent(tcChatModel, toolset)
	if err != nil {
		return nil, err
	}

	multiAgent, err := agent.NewMultiAgentSystem(tcChatModel, cfg.Ark.APIKey, cfg.Ark.BaseURL, cfg.Ark.Model, milvusClient, embeddingSvc)
	if err != nil {
		return nil, err
	}

	ragAgent, err := agent.NewRAGAgent(tcChatModel, toolset, ragService.BuildContext)
	if err != nil {
		return nil, err
	}

	ragGraph, err := graph.NewRAGGraph(&graph.RAGGraphConfig{
		ChatModel:    baseChatModel, // 传入基础 ChatModel 接口
		APIKey:       cfg.Ark.APIKey,
		BaseURL:      cfg.Ark.BaseURL,
		Model:        cfg.Ark.Model,
		RAGContext:   ragService.BuildContext,
		SystemPrompt: "你是资深网络热词顾问，必须引用参考资料作答。",
	})
	if err != nil {
		return nil, err
	}

	graphToolHandlers := map[string]func(context.Context, string) (string, error){
		"search": func(ctx context.Context, query string) (string, error) {
			return multiAgent.ProcessQuery(ctx, query, "search")
		},
		"analysis": func(ctx context.Context, query string) (string, error) {
			return multiAgent.ProcessQuery(ctx, query, "analysis")
		},
		"explanation": func(ctx context.Context, query string) (string, error) {
			return multiAgent.ProcessQuery(ctx, query, "explanation")
		},
		"comprehensive": func(ctx context.Context, query string) (string, error) {
			return multiAgent.ProcessQuery(ctx, query, "comprehensive")
		},
	}

	multiGraph, err := graph.NewMultiStageGraph(&graph.MultiStageGraphConfig{
		ChatModel:  baseChatModel, // 传入基础 ChatModel 接口
		APIKey:     cfg.Ark.APIKey,
		BaseURL:    cfg.Ark.BaseURL,
		Model:      cfg.Ark.Model,
		RAGContext: ragService.BuildContext,
		Tools:      graphToolHandlers,
	})
	if err != nil {
		return nil, err
	}

	return &Handler{
		cfg:          cfg,
		ragService:   ragService,
		chatModel:    baseChatModel,
		milvusClient: milvusClient,
		reactAgent:   reactAgent,
		multiAgent:   multiAgent,
		ragAgent:     ragAgent,
		ragGraph:     ragGraph,
		multiGraph:   multiGraph,
	}, nil
}

func (h *Handler) Close() {
	if h.ragService != nil {
		h.ragService.Close()
	}
	if h.milvusClient != nil {
		h.milvusClient.Close()
	}
}

type UploadResponse struct {
	Message  string `json:"message"`
	Filename string `json:"filename"`
}

func (h *Handler) HandleUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件上传失败"})
		return
	}

	if file.Size > h.cfg.Upload.MaxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件大小超过限制"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".md" && ext != ".markdown" && ext != ".pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .md / .markdown / .pdf"})
		return
	}
	contentType, err := detectUploadedContentType(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法识别文件类型"})
		return
	}
	if !isAllowedUploadType(ext, contentType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件内容类型与扩展名不匹配"})
		return
	}

	if err := os.MkdirAll(h.cfg.Upload.Dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建上传目录失败"})
		return
	}

	now := time.Now()
	safeName := sanitizeUploadFilename(file.Filename)
	filename := fmt.Sprintf("%d_%s", now.Unix(), safeName)
	savePath := filepath.Join(h.cfg.Upload.Dir, filename)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}

	metadata := fmt.Sprintf("filename:%s,upload_time:%s", safeName, now.Format(time.RFC3339))
	if err := h.ragService.IndexFile(c.Request.Context(), savePath, metadata); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("索引文件失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, UploadResponse{
		Message:  "文件上传并入库成功",
		Filename: filename,
	})
}

type QueryRequest struct {
	Query string `json:"query" binding:"required"`
	Mode  string `json:"mode"`
}

func (h *Handler) HandleQuery(c *gin.Context) {
	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "rag"
	}

	flusher, ok := prepareSSE(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	switch mode {
	case "rag":
		h.handleRAGMode(ctx, c, flusher, req.Query)
	case "react":
		// 注意：Eino ReAct Agent 的流式模式在工具调用后不会继续输出结果
		// 因此这里改用非流式模式，一次性返回完整结果
		h.streamTextResult(c, flusher, func(ctx context.Context) (string, error) {
			return h.reactAgent.Run(ctx, req.Query)
		})
	case "rag_agent":
		h.streamTextResult(c, flusher, func(ctx context.Context) (string, error) {
			return h.ragAgent.Run(ctx, req.Query)
		})
	case "multi-agent":
		h.streamTextResult(c, flusher, func(ctx context.Context) (string, error) {
			return h.multiAgent.ProcessQuery(ctx, req.Query, "comprehensive")
		})
	case "graph_rag":
		stream, err := h.ragGraph.Stream(ctx, req.Query)
		if err != nil {
			h.writeSSEError(c, flusher, fmt.Sprintf("RAG Graph 失败: %v", err))
			return
		}
		h.streamMessageReader(c, flusher, stream)
	case "graph_multi":
		h.streamTextResult(c, flusher, func(ctx context.Context) (string, error) {
			return h.multiGraph.Run(ctx, req.Query)
		})
	default:
		h.writeSSEError(c, flusher, fmt.Sprintf("不支持的模式: %s", mode))
	}
}

type SearchRequest struct {
	Query string `json:"query" binding:"required"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type SearchResult struct {
	Content  string  `json:"content"`
	Metadata string  `json:"metadata"`
	Score    float32 `json:"score"`
}

func (h *Handler) HandleSearch(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}

	results, err := h.ragService.Search(c.Request.Context(), req.Query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("检索失败: %v", err)})
		return
	}

	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			Content:  r.Content,
			Metadata: r.Metadata,
			Score:    r.Score,
		}
	}

	c.JSON(http.StatusOK, SearchResponse{
		Results: searchResults,
	})
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (h *Handler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{
		Status:  "ok",
		Version: "1.0.0-ark-extended",
	})
}

func (h *Handler) handleRAGMode(ctx context.Context, c *gin.Context, flusher http.Flusher, query string) {
	// 使用智能检索：优先本地知识，必要时联网搜索
	ragContext, fromWeb, err := h.ragService.SmartRetrieve(ctx, query)
	if err != nil {
		h.writeSSEError(c, flusher, fmt.Sprintf("智能检索失败: %v", err))
		return
	}

	// 如果使用了联网搜索，添加提示并直接返回搜索结果
	if fromWeb {
		h.writeSSEMessage(c, flusher, "ℹ️ 本地知识库暂无相关内容，已为您联网搜索最新信息...\n\n")
		// 联网搜索结果已经是完整答案，直接返回
		h.writeSSEMessage(c, flusher, ragContext)
		h.writeSSEDone(c, flusher)
		return
	}

	// 使用本地知识，正常RAG流程
	messages := []*schema.Message{
		{
			Role:    schema.System,
			Content: "你是网络热词助手，必须依据参考资料回答问题。",
		},
		{
			Role: schema.User,
			Content: fmt.Sprintf(`
【参考文档】
%s

【用户问题】
%s

请基于参考信息返回逐步答案。`, ragContext, query),
		},
	}

	streamReader, err := h.chatModel.Stream(ctx, messages)
	if err != nil {
		h.writeSSEError(c, flusher, fmt.Sprintf("LLM 调用失败: %v", err))
		return
	}

	h.streamMessageReader(c, flusher, streamReader)
}

func (h *Handler) streamMessageReader(c *gin.Context, flusher http.Flusher, reader *schema.StreamReader[*schema.Message]) {
	defer reader.Close()

	toolCallsShown := make(map[string]bool) // 记录已显示的工具
	buffer := ""                            // 内容缓存
	flushThreshold := 50                    // 缓存字符数阙值
	chunkCount := 0                         // chunk 计数

	for {
		chunk, err := reader.Recv()
		chunkCount++

		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Info("[StreamReader] 流结束", zap.Int("totalChunks", chunkCount))
				// 输出剩余缓存
				if buffer != "" {
					h.writeSSEMessage(c, flusher, buffer)
				}
				h.writeSSEDone(c, flusher)
				break
			}
			h.writeSSEError(c, flusher, fmt.Sprintf("读取流失败: %v", err))
			break
		}

		if chunk != nil {
			logger.Debug("[StreamReader] 收到chunk",
				zap.Int("chunkNum", chunkCount),
				zap.String("role", string(chunk.Role)),
				zap.Int("contentLen", len(chunk.Content)),
				zap.Int("toolCallsCount", len(chunk.ToolCalls)))

			// 检测到工具调用 - 检查并清空缓存
			if len(chunk.ToolCalls) > 0 {
				// 检查缓存是否为工具调用描述
				if buffer != "" && isToolCallDescription(buffer) {
					logger.Debug("[StreamReader] 丢弃工具调用描述", zap.String("buffer", buffer))
					// 是工具调用描述，丢弃缓存
					buffer = ""
				} else if buffer != "" {
					// 不是工具调用描述，输出缓存
					logger.Debug("[StreamReader] 输出缓存内容", zap.String("buffer", buffer))
					h.writeSSEMessage(c, flusher, buffer)
					buffer = ""
				}

				for _, toolCall := range chunk.ToolCalls {
					toolID := toolCall.ID
					toolName := toolCall.Function.Name

					// 只在工具名称完整时显示
					if toolID != "" && toolName != "" && strings.Contains(toolName, "_") && !toolCallsShown[toolID] {
						toolInfo := fmt.Sprintf("\n\n🔧 正在调用工具: %s ...\n\n", toolName)
						h.writeSSEMessage(c, flusher, toolInfo)
						toolCallsShown[toolID] = true
						logger.Info("[StreamReader] 显示工具调用", zap.String("tool", toolName))
					}
				}
				continue
			}

			// 处理内容 - 始终先缓存
			if chunk.Content != "" {
				buffer += chunk.Content

				// 检查缓存内容
				// 1. 如果遇到句号，判断是否为工具调用描述
				if strings.Contains(buffer, "。") {
					if isToolCallDescription(buffer) {
						logger.Debug("[StreamReader] 检测到工具调用描述，丢弃", zap.String("buffer", buffer))
						// 是工具调用描述，丢弃
						buffer = ""
					} else {
						// 不是工具调用描述，输出
						h.writeSSEMessage(c, flusher, buffer)
						buffer = ""
					}
					continue
				}

				// 2. 如果缓存太长且不像工具调用描述，输出
				if len(buffer) > flushThreshold && !strings.Contains(buffer, "调用") && !strings.Contains(buffer, "函数") {
					h.writeSSEMessage(c, flusher, buffer)
					buffer = ""
				}
			}
		}
	}
}

// isToolCallDescription 判断文本是否为工具调用描述
func isToolCallDescription(text string) bool {
	for _, pattern := range toolCallDescriptionPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func sanitizeUploadFilename(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "" || base == "." {
		return "upload_file"
	}

	re := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	safe := re.ReplaceAllString(base, "_")
	safe = strings.Trim(safe, " .")
	if safe == "" {
		return "upload_file"
	}

	const maxFilenameLen = 128
	runes := []rune(safe)
	if len(runes) > maxFilenameLen {
		safe = string(runes[:maxFilenameLen])
	}

	return safe
}

func detectUploadedContentType(fileHeader *multipart.FileHeader) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return http.DetectContentType(header[:n]), nil
}

func isAllowedUploadType(ext, contentType string) bool {
	switch ext {
	case ".pdf":
		return contentType == "application/pdf"
	case ".md", ".markdown":
		return strings.HasPrefix(contentType, "text/") ||
			contentType == "application/octet-stream" ||
			contentType == "application/x-empty"
	default:
		return false
	}
}

func (h *Handler) streamTextResult(c *gin.Context, flusher http.Flusher, fn func(context.Context) (string, error)) {
	result, err := fn(c.Request.Context())
	if err != nil {
		h.writeSSEError(c, flusher, err.Error())
		return
	}

	h.writeSSEMessage(c, flusher, result)
	h.writeSSEDone(c, flusher)
}

func prepareSSE(c *gin.Context) (http.Flusher, bool) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "当前响应不支持 SSE"})
		return nil, false
	}
	return flusher, true
}

func (h *Handler) writeSSEMessage(c *gin.Context, flusher http.Flusher, content string) {
	c.SSEvent("message", gin.H{"content": content})
	flusher.Flush()
}

func (h *Handler) writeSSEError(c *gin.Context, flusher http.Flusher, message string) {
	c.SSEvent("error", gin.H{"message": message})
	flusher.Flush()
}

func (h *Handler) writeSSEDone(c *gin.Context, flusher http.Flusher) {
	c.SSEvent("done", gin.H{"message": "完成"})
	flusher.Flush()
}
