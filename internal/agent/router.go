package agent

import (
	"context"
	"strings"
)

type DefaultRouter struct{}

func NewRouter() *DefaultRouter {
	return &DefaultRouter{}
}

func (r *DefaultRouter) Resolve(ctx context.Context, query, requestedMode string) (AgentRoute, error) {
	_ = ctx
	requestedMode = normalizeMode(requestedMode)
	if requestedMode != "" {
		return routeForMode(query, requestedMode, "尊重用户显式指定的 mode"), nil
	}

	complexity := detectComplexity(query)
	switch {
	case isComprehensiveQuery(query) && complexity == "complex":
		return routeForMode(query, ModeGraphMulti, "复杂综合问题，优先使用 graph_multi 做结构化规划"), nil
	case isComprehensiveQuery(query):
		return routeForMode(query, ModeMultiAgent, "综合问题，优先使用 multi-agent 聚合多视角结果"), nil
	case needsTooling(query):
		return routeForMode(query, ModeRAGAgent, "问题需要联网、最新信息或工具辅助，优先使用 rag_agent"), nil
	default:
		return routeForMode(query, ModeRAG, "普通问答优先走 rag，降低延迟与复杂度"), nil
	}
}

func routeForMode(query, mode, reason string) AgentRoute {
	return AgentRoute{
		ResolvedMode: mode,
		Reason:       reason,
		NeedTools:    mode == ModeRAGAgent || mode == ModeReact || mode == ModeMultiAgent || mode == ModeGraphMulti,
		NeedRAG:      mode != ModeReact,
		NeedPlanning: mode == ModeMultiAgent || mode == ModeGraphMulti,
		Complexity:   detectComplexity(query),
	}
}

func normalizeMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func detectComplexity(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "simple"
	}
	complexSignals := []string{"以及", "并且", "分别", "步骤", "总结", "对比", "区别", "为什么", "怎么", "如何", "原因", "影响", "案例"}
	count := 0
	for _, signal := range complexSignals {
		if strings.Contains(query, signal) {
			count++
		}
	}
	switch {
	case count >= 3 || strings.Count(query, "？")+strings.Count(query, "?") >= 2:
		return "complex"
	case count >= 1:
		return "medium"
	default:
		return "simple"
	}
}

func needsTooling(query string) bool {
	toolSignals := []string{"最新", "最近", "今天", "本周", "本月", "实时", "联网", "搜索", "数据", "热度", "趋势"}
	for _, signal := range toolSignals {
		if strings.Contains(query, signal) {
			return true
		}
	}
	return false
}

func isComprehensiveQuery(query string) bool {
	comprehensiveSignals := []string{"为什么", "怎么用", "起源", "背景", "影响", "详细", "全面", "总结", "分析", "解释", "会不会"}
	count := 0
	for _, signal := range comprehensiveSignals {
		if strings.Contains(query, signal) {
			count++
		}
	}
	return count >= 2
}
