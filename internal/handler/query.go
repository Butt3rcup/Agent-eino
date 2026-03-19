package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

type QueryRequest struct {
	Query string `json:"query" binding:"required"`
	Mode  string `json:"mode"`
}

type SearchRequest struct {
	Query string `json:"query" binding:"required"`
}

type SearchResponse struct {
	Matched bool           `json:"matched"`
	Reason  string         `json:"reason,omitempty"`
	Count   int            `json:"count"`
	Results []SearchResult `json:"results"`
}

type SearchResult struct {
	Content  string  `json:"content"`
	Metadata string  `json:"metadata"`
	Score    float32 `json:"score"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
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
	for i, result := range results {
		searchResults[i] = SearchResult{
			Content:  result.Content,
			Metadata: result.Metadata,
			Score:    result.Score,
		}
	}

	resp := SearchResponse{
		Matched: len(searchResults) > 0,
		Count:   len(searchResults),
		Results: searchResults,
	}
	if len(searchResults) == 0 {
		resp.Reason = "no_relevant_match"
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{
		Status:  "ok",
		Version: "1.0.0-ark-extended",
	})
}

func (h *Handler) handleRAGMode(ctx context.Context, c *gin.Context, flusher http.Flusher, query string) {
	ragContext, fromWeb, err := h.ragService.SmartRetrieve(ctx, query)
	if err != nil {
		h.writeSSEError(c, flusher, fmt.Sprintf("智能检索失败: %v", err))
		return
	}

	if fromWeb {
		h.writeSSEMessage(c, flusher, "ℹ️ 本地知识库暂无相关内容，已为您联网搜索最新信息...\n\n")
		h.writeSSEMessage(c, flusher, ragContext)
		h.writeSSEDone(c, flusher)
		return
	}

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
