package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/sync/errgroup"

	hotwordTool "go-eino-agent/internal/tool"
	"go-eino-agent/pkg/embedding"
	"go-eino-agent/pkg/vectordb"
)

type ReActAgent struct {
	agent     *react.Agent
	mode      string
	validator Validator
}

func NewReActAgent(ctx context.Context, chatModel model.ToolCallingChatModel, mode string, tools []tool.BaseTool, validator Validator) (*ReActAgent, error) {
	guardedTools, err := GuardTools(ctx, mode, validator, tools)
	if err != nil {
		return nil, fmt.Errorf("failed to guard tools: %w", err)
	}
	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      buildToolsConfig(guardedTools),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create react agent: %w", err)
	}

	return &ReActAgent{agent: reactAgent, mode: mode, validator: validator}, nil
}

func (a *ReActAgent) Run(ctx context.Context, query string) (string, error) {
	input := []*schema.Message{{Role: schema.User, Content: query}}
	output, err := a.agent.Generate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("agent generation failed: %w", err)
	}
	if output == nil || strings.TrimSpace(output.Content) == "" {
		return "", fmt.Errorf("empty response from agent")
	}
	if err := validateAgentAnswer(ctx, a.validator, query, output.Content, ""); err != nil {
		return "", err
	}
	return output.Content, nil
}

func (a *ReActAgent) Stream(ctx context.Context, query string) (*schema.StreamReader[*schema.Message], error) {
	input := []*schema.Message{{Role: schema.User, Content: query}}
	return a.agent.Stream(ctx, input)
}

type MultiAgentSystem struct {
	searchAgent      *ReActAgent
	analysisAgent    *ReActAgent
	explanationAgent *ReActAgent
	planner          Planner
	validator        Validator
}

func NewMultiAgentSystem(ctx context.Context, chatModel model.ToolCallingChatModel, vectorDB *vectordb.MilvusClient, embeddingSvc *embedding.Service, webSearchTool tool.BaseTool, planner Planner, validator Validator) (*MultiAgentSystem, error) {
	searchTools := []tool.BaseTool{hotwordTool.NewHotwordSearchTool(vectorDB, embeddingSvc)}
	if webSearchTool != nil {
		searchTools = append(searchTools, webSearchTool)
	}

	searchAgent, err := NewReActAgent(ctx, chatModel, ModeMultiAgent, searchTools, validator)
	if err != nil {
		return nil, fmt.Errorf("failed to create search agent: %w", err)
	}

	analysisAgent, err := NewReActAgent(ctx, chatModel, ModeMultiAgent, []tool.BaseTool{hotwordTool.NewTrendAnalysisTool()}, validator)
	if err != nil {
		return nil, fmt.Errorf("failed to create analysis agent: %w", err)
	}

	explanationAgent, err := NewReActAgent(ctx, chatModel, ModeMultiAgent, []tool.BaseTool{hotwordTool.NewExplanationTool()}, validator)
	if err != nil {
		return nil, fmt.Errorf("failed to create explanation agent: %w", err)
	}

	return &MultiAgentSystem{
		searchAgent:      searchAgent,
		analysisAgent:    analysisAgent,
		explanationAgent: explanationAgent,
		planner:          planner,
		validator:        validator,
	}, nil
}

func (m *MultiAgentSystem) ProcessQuery(ctx context.Context, query string, queryType string) (string, error) {
	switch queryType {
	case "search":
		return m.searchAgent.Run(ctx, query)
	case "analysis":
		return m.analysisAgent.Run(ctx, query)
	case "explanation":
		return m.explanationAgent.Run(ctx, query)
	case "comprehensive":
		return m.PlanAndProcess(ctx, query)
	default:
		return "", fmt.Errorf("unknown query type: %s", queryType)
	}
}

func (m *MultiAgentSystem) PlanAndProcess(ctx context.Context, query string) (string, error) {
	plan, err := m.planner.BuildPlan(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to build execution plan: %w", err)
	}
	if trace := TraceFromContext(ctx); trace != nil {
		trace.MarkPlannerUsed()
	}

	results := make(map[string]string, len(plan.Steps))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	for _, step := range plan.Steps {
		if !step.Required || step.Name == "summarize" {
			continue
		}
		currentStep := step
		g.Go(func() error {
			res, err := m.executeStep(gctx, currentStep.Name, query)
			if err != nil {
				return fmt.Errorf("%s failed: %w", currentStep.Name, err)
			}
			mu.Lock()
			results[currentStep.Name] = res
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return "", err
	}

	summary := summarizePlanResults(plan, results)
	if err := validateAgentAnswer(ctx, m.validator, query, summary, strings.Join(mapValues(results), "\n\n")); err != nil {
		return "", err
	}
	return summary, nil
}

func (m *MultiAgentSystem) executeStep(ctx context.Context, stepName, query string) (string, error) {
	switch stepName {
	case "search":
		return m.searchAgent.Run(ctx, fmt.Sprintf("搜索热词：%s", query))
	case "analysis":
		return m.analysisAgent.Run(ctx, fmt.Sprintf("分析热词趋势：%s", query))
	case "explanation":
		return m.explanationAgent.Run(ctx, fmt.Sprintf("详细解释热词：%s", query))
	default:
		return "", fmt.Errorf("unsupported plan step: %s", stepName)
	}
}

func summarizePlanResults(plan ExecutionPlan, results map[string]string) string {
	sections := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if !step.Required || step.Name == "summarize" {
			continue
		}
		result := strings.TrimSpace(results[step.Name])
		if result == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("【%s】%s", stepDisplayName(step.Name), result))
	}
	if len(sections) == 0 {
		return "未能生成有效的多阶段分析结果。"
	}
	return fmt.Sprintf("=== 综合分析报告 ===\n\n%s", strings.Join(sections, "\n\n"))
}

func stepDisplayName(stepName string) string {
	switch stepName {
	case "search":
		return "检索结果"
	case "analysis":
		return "趋势分析"
	case "explanation":
		return "详细解释"
	default:
		return stepName
	}
}

type RAGAgent struct {
	agent      *react.Agent
	ragContext func(context.Context, string) (string, error)
	validator  Validator
}

func NewRAGAgent(ctx context.Context, chatModel model.ToolCallingChatModel, mode string, tools []tool.BaseTool, ragContextFunc func(context.Context, string) (string, error), validator Validator) (*RAGAgent, error) {
	guardedTools, err := GuardTools(ctx, mode, validator, tools)
	if err != nil {
		return nil, fmt.Errorf("failed to guard tools: %w", err)
	}
	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      buildToolsConfig(guardedTools),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return &RAGAgent{agent: reactAgent, ragContext: ragContextFunc, validator: validator}, nil
}

func (a *RAGAgent) Run(ctx context.Context, query string) (string, error) {
	ragContext, err := a.ragContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to get RAG context: %w", err)
	}

	enhancedQuery := fmt.Sprintf("%s\n\n【参考文档】%s\n\n【用户问题】%s\n\n如果参考里没有答案，可以调用可用工具继续检索。", GroundedAnswerPrompt, ragContext, query)
	input := []*schema.Message{{Role: schema.User, Content: enhancedQuery}}
	output, err := a.agent.Generate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("agent generation failed: %w", err)
	}
	if output == nil || strings.TrimSpace(output.Content) == "" {
		return "", fmt.Errorf("empty response from agent")
	}
	if err := validateAgentAnswer(ctx, a.validator, query, output.Content, ragContext); err != nil {
		return "", err
	}
	return output.Content, nil
}

type SimpleAgent struct {
	model model.ChatModel
}

func NewSimpleAgent(chatModel model.ChatModel) *SimpleAgent {
	return &SimpleAgent{model: chatModel}
}

func (a *SimpleAgent) Chat(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	output, err := a.model.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("chat failed: %w", err)
	}
	return output, nil
}

func buildToolsConfig(tools []tool.BaseTool) compose.ToolsNodeConfig {
	if len(tools) == 0 {
		return compose.ToolsNodeConfig{}
	}

	cfg := compose.ToolsNodeConfig{Tools: make([]tool.BaseTool, len(tools))}
	copy(cfg.Tools, tools)
	return cfg
}

func validateAgentAnswer(ctx context.Context, validator Validator, query, answer, evidence string) error {
	if validator == nil {
		return nil
	}
	if err := validator.ValidateAnswer(query, answer, evidence); err != nil {
		if trace := TraceFromContext(ctx); trace != nil {
			trace.IncValidationFailures()
		}
		return err
	}
	return nil
}

func mapValues(values map[string]string) []string {
	results := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			results = append(results, value)
		}
	}
	return results
}
