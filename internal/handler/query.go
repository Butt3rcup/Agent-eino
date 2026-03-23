package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"go-eino-agent/internal/agent"
)

const (
	QueryModeContextKey               = "query_mode"
	QueryRequestedModeContextKey      = "query_requested_mode"
	QueryRouteReasonContextKey        = "query_route_reason"
	QueryFallbackUsedContextKey       = "query_fallback_used"
	QueryPlannerUsedContextKey        = "query_planner_used"
	QueryToolCallsContextKey          = "query_tool_calls"
	QueryToolFailuresContextKey       = "query_tool_failures"
	QueryValidationFailuresContextKey = "query_validation_failures"
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
	Status             string                    `json:"status"`
	Version            string                    `json:"version"`
	Ready              bool                      `json:"ready"`
	DegradedComponents []string                  `json:"degraded_components,omitempty"`
	Components         map[string]ComponentState `json:"components,omitempty"`
	Modes              map[string]ModeState      `json:"modes,omitempty"`
	Background         any                       `json:"background,omitempty"`
}

func (h *Handler) HandleQuery(c *gin.Context) {
	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效"})
		return
	}

	requestedMode := strings.ToLower(strings.TrimSpace(req.Mode))
	trace := agent.NewRequestTrace(requestedMode)
	ctx := agent.WithRequestTrace(c.Request.Context(), trace)
	c.Request = c.Request.WithContext(ctx)
	defer h.syncTraceContext(c, trace)

	route, err := h.router.Resolve(ctx, req.Query, requestedMode)
	if err != nil {
		route = agent.AgentRoute{RequestedMode: requestedMode, ResolvedMode: fallbackDefaultMode(requestedMode), Reason: "router 解析失败，已回退到默认模式"}
	}
	resolvedMode := h.resolveMode(route, requestedMode)
	trace.SetRoute(resolvedMode, route.Reason)
	c.Set(QueryRequestedModeContextKey, requestedMode)
	c.Set(QueryRouteReasonContextKey, route.Reason)
	c.Set(QueryModeContextKey, resolvedMode)

	cancel, ok := h.applyQueryPolicy(c, resolvedMode)
	if !ok {
		return
	}
	defer cancel()

	flusher, ok := prepareSSE(c)
	if !ok {
		return
	}

	h.runModeWithFallback(c.Request.Context(), c, flusher, resolvedMode, req.Query)
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
		searchResults[i] = SearchResult{Content: result.Content, Metadata: result.Metadata, Score: result.Score}
	}

	resp := SearchResponse{Matched: len(searchResults) > 0, Count: len(searchResults), Results: searchResults}
	if len(searchResults) == 0 {
		resp.Reason = "no_relevant_match"
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) HandleHealth(c *gin.Context) {
	snapshot := h.runtimeStatus.Snapshot(h.ragService.QueueStats())
	c.JSON(http.StatusOK, HealthResponse{
		Status:             snapshot.Status,
		Version:            "1.2.0",
		Ready:              snapshot.Ready,
		DegradedComponents: snapshot.DegradedComponents,
		Components:         snapshot.Components,
		Modes:              snapshot.Modes,
		Background: map[string]any{
			"knowledge_persist": snapshot.Background,
			"upload_indexing":   h.uploadTasks.Stats(),
			"query_cache":       h.ragService.CacheStats(),
			"query_policies":    h.QueryPolicySnapshot(),
			"agent_runtime":     h.agentRuntimeSnapshot(),
		},
	})
}

func (h *Handler) handleRAGMode(ctx context.Context, c *gin.Context, flusher http.Flusher, query string) error {
	ragContext, fromWeb, err := h.ragService.SmartRetrieve(ctx, query)
	if err != nil {
		return fmt.Errorf("智能检索失败: %s", friendlyQueryError(err))
	}

	if fromWeb {
		h.writeSSEMessage(c, flusher, "ℹ️ 本地知识库暂无相关内容，已为你联网搜索最新信息。\n\n")
		h.writeSSEMessage(c, flusher, ragContext)
		h.writeSSEDone(c, flusher)
		return nil
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: agent.GroundedAnswerPrompt},
		{Role: schema.User, Content: fmt.Sprintf("【参考文档】%s\n\n【用户问题】%s\n\n请基于参考信息返回逐步答案。", ragContext, query)},
	}

	streamReader, err := h.chatModel.Stream(ctx, messages)
	if err != nil {
		return fmt.Errorf("LLM 调用失败: %s", friendlyQueryError(err))
	}

	h.streamMessageReader(c, flusher, streamReader)
	return nil
}

func (h *Handler) ensureModeAvailable(mode string) (bool, string) {
	state, ok := h.runtimeStatus.Mode(mode)
	if !ok {
		return true, ""
	}
	if state.Available {
		return true, ""
	}
	if state.Reason != "" {
		return false, state.Reason
	}
	return false, fmt.Sprintf("模式 %s 当前不可用", mode)
}

func (h *Handler) resolveMode(route agent.AgentRoute, requestedMode string) string {
	mode := strings.TrimSpace(route.ResolvedMode)
	if mode == "" {
		mode = fallbackDefaultMode(requestedMode)
	}
	if !isSupportedQueryMode(mode) {
		mode = agent.ModeRAG
	}
	if ok, _ := h.ensureModeAvailable(mode); ok {
		return mode
	}
	if next, ok := h.resolveFallbackMode(mode); ok {
		return next
	}
	return agent.ModeRAG
}

func fallbackDefaultMode(requestedMode string) string {
	if strings.TrimSpace(requestedMode) != "" {
		return strings.TrimSpace(requestedMode)
	}
	return agent.ModeRAG
}

func isSupportedQueryMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case agent.ModeRAG, agent.ModeReact, agent.ModeRAGAgent, agent.ModeMultiAgent, agent.ModeGraphRAG, agent.ModeGraphMulti:
		return true
	default:
		return false
	}
}

func (h *Handler) runModeWithFallback(ctx context.Context, c *gin.Context, flusher http.Flusher, mode, query string) {
	if err := h.executeMode(ctx, c, flusher, mode, query); err == nil {
		return
	} else {
		nextMode, ok := h.resolveFallbackMode(mode)
		if !ok {
			h.writeSSEError(c, flusher, friendlyQueryError(err))
			return
		}
		trace := agent.TraceFromContext(ctx)
		if trace != nil && trace.Snapshot().FallbackUsed {
			h.writeSSEError(c, flusher, friendlyQueryError(err))
			return
		}
		if trace != nil {
			trace.MarkFallback(mode, nextMode)
		}
		c.Set(QueryModeContextKey, nextMode)
		h.writeSSEMessage(c, flusher, fmt.Sprintf("ℹ️ 当前模式 %s 执行不稳定，已自动降级为 %s。\n\n", mode, nextMode))
		if fallbackErr := h.executeMode(ctx, c, flusher, nextMode, query); fallbackErr != nil {
			h.writeSSEError(c, flusher, friendlyQueryError(fallbackErr))
		}
	}
}

func (h *Handler) executeMode(ctx context.Context, c *gin.Context, flusher http.Flusher, mode, query string) error {
	switch mode {
	case agent.ModeRAG:
		return h.handleRAGMode(ctx, c, flusher, query)
	case agent.ModeReact:
		return h.writeTextMode(ctx, c, flusher, func(ctx context.Context) (string, error) { return h.reactAgent.Run(ctx, query) })
	case agent.ModeRAGAgent:
		return h.writeTextMode(ctx, c, flusher, func(ctx context.Context) (string, error) { return h.ragAgent.Run(ctx, query) })
	case agent.ModeMultiAgent:
		return h.writeTextMode(ctx, c, flusher, func(ctx context.Context) (string, error) {
			return h.multiAgent.ProcessQuery(ctx, query, "comprehensive")
		})
	case agent.ModeGraphRAG:
		return h.writeTextMode(ctx, c, flusher, func(ctx context.Context) (string, error) {
			message, err := h.ragGraph.Run(ctx, query)
			if err != nil {
				return "", err
			}
			if message == nil {
				return "", fmt.Errorf("graph_rag 返回了空结果")
			}
			return message.Content, nil
		})
	case agent.ModeGraphMulti:
		return h.writeTextMode(ctx, c, flusher, func(ctx context.Context) (string, error) { return h.multiGraph.Run(ctx, query) })
	default:
		return fmt.Errorf("不支持的模式: %s", mode)
	}
}

func (h *Handler) writeTextMode(ctx context.Context, c *gin.Context, flusher http.Flusher, fn func(context.Context) (string, error)) error {
	result, err := fn(ctx)
	if err != nil {
		return err
	}
	h.writeSSEMessage(c, flusher, result)
	h.writeSSEDone(c, flusher)
	return nil
}

func (h *Handler) resolveFallbackMode(mode string) (string, bool) {
	nextMode, ok := h.fallback.NextMode(mode)
	if !ok {
		return "", false
	}
	if available, _ := h.ensureModeAvailable(nextMode); available {
		return nextMode, true
	}
	return "", false
}

func (h *Handler) syncTraceContext(c *gin.Context, trace *agent.RequestTrace) {
	snapshot := trace.Snapshot()
	c.Set(QueryRequestedModeContextKey, snapshot.RequestedMode)
	c.Set(QueryModeContextKey, snapshot.ResolvedMode)
	c.Set(QueryRouteReasonContextKey, snapshot.RouteReason)
	c.Set(QueryFallbackUsedContextKey, snapshot.FallbackUsed)
	c.Set(QueryPlannerUsedContextKey, snapshot.PlannerUsed)
	c.Set(QueryToolCallsContextKey, snapshot.ToolCalls)
	c.Set(QueryToolFailuresContextKey, snapshot.ToolFailures)
	c.Set(QueryValidationFailuresContextKey, snapshot.ValidationFailures)
	if snapshot.FallbackUsed {
		c.Set("query_fallback_from", snapshot.FallbackFrom)
		c.Set("query_fallback_to", snapshot.FallbackTo)
	}
}

func (h *Handler) agentRuntimeSnapshot() map[string]bool {
	return map[string]bool{
		"router_enabled":    h.router != nil,
		"planner_enabled":   h.planner != nil,
		"validator_enabled": h.validator != nil,
		"fallback_enabled":  h.fallback != nil,
	}
}
