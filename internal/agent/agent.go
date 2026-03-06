package agent

import (
	"context"
	"fmt"
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

// ReActAgent 使用新版 react.Agent 封装的工具调度 agent。
type ReActAgent struct {
	agent *react.Agent
}

// NewReActAgent 使用外部传入的 ToolCallingChatModel 创建 ReAct Agent，避免重复创建模型实例。
func NewReActAgent(chatModel model.ToolCallingChatModel, tools []tool.BaseTool) (*ReActAgent, error) {
	reactAgent, err := react.NewAgent(context.Background(), &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      buildToolsConfig(tools),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create react agent: %w", err)
	}

	return &ReActAgent{agent: reactAgent}, nil
}

func (a *ReActAgent) Run(ctx context.Context, query string) (string, error) {
	input := []*schema.Message{
		{
			Role:    schema.User,
			Content: query,
		},
	}

	output, err := a.agent.Generate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("agent generation failed: %w", err)
	}
	if output == nil || output.Content == "" {
		return "", fmt.Errorf("empty response from agent")
	}
	return output.Content, nil
}

func (a *ReActAgent) Stream(ctx context.Context, query string) (*schema.StreamReader[*schema.Message], error) {
	input := []*schema.Message{
		{
			Role:    schema.User,
			Content: query,
		},
	}

	return a.agent.Stream(ctx, input)
}

// MultiAgentSystem 组合多个 ReAct agent 完成协作。
type MultiAgentSystem struct {
	searchAgent      *ReActAgent
	analysisAgent    *ReActAgent
	explanationAgent *ReActAgent
}

// NewMultiAgentSystem 使用外部传入的 ToolCallingChatModel 创建多 Agent 系统，避免重复创建模型实例。
func NewMultiAgentSystem(chatModel model.ToolCallingChatModel, apiKey, baseURL, modelName string, vectorDB *vectordb.MilvusClient, embeddingSvc *embedding.Service) (*MultiAgentSystem, error) {
	// 为搜索 Agent 创建火山引擎联网搜索工具
	webSearchTool, err := hotwordTool.NewVolcanoWebSearchTool(apiKey, baseURL, modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to create web search tool: %w", err)
	}

	searchAgent, err := NewReActAgent(chatModel, []tool.BaseTool{
		hotwordTool.NewHotwordSearchTool(vectorDB, embeddingSvc),
		webSearchTool,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create search agent: %w", err)
	}

	analysisAgent, err := NewReActAgent(chatModel, []tool.BaseTool{
		hotwordTool.NewTrendAnalysisTool(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create analysis agent: %w", err)
	}

	explanationAgent, err := NewReActAgent(chatModel, []tool.BaseTool{
		hotwordTool.NewExplanationTool(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create explanation agent: %w", err)
	}

	return &MultiAgentSystem{
		searchAgent:      searchAgent,
		analysisAgent:    analysisAgent,
		explanationAgent: explanationAgent,
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
		return m.comprehensiveProcess(ctx, query)
	default:
		return "", fmt.Errorf("unknown query type: %s", queryType)
	}
}

func (m *MultiAgentSystem) comprehensiveProcess(ctx context.Context, query string) (string, error) {
	var (
		mu                sync.Mutex
		searchResult      string
		analysisResult    string
		explanationResult string
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		res, err := m.searchAgent.Run(gctx, fmt.Sprintf("搜索热词: %s", query))
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}
		mu.Lock()
		searchResult = res
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		res, err := m.analysisAgent.Run(gctx, fmt.Sprintf("分析热词趋势: %s", query))
		if err != nil {
			return fmt.Errorf("analysis failed: %w", err)
		}
		mu.Lock()
		analysisResult = res
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		res, err := m.explanationAgent.Run(gctx, fmt.Sprintf("详细解读热词: %s", query))
		if err != nil {
			return fmt.Errorf("explanation failed: %w", err)
		}
		mu.Lock()
		explanationResult = res
		mu.Unlock()
		return nil
	})

	if err := g.Wait(); err != nil {
		return "", err
	}

	result := fmt.Sprintf(`
=== 综合分析报告 ===

【检索结果】
%s

【趋势分析】
%s

【详细解读】
%s
`, searchResult, analysisResult, explanationResult)

	return result, nil
}

// RAGAgent：先检索上下文，再通过 ReActAgent 作答。
type RAGAgent struct {
	agent      *react.Agent
	ragContext func(context.Context, string) (string, error)
}

// NewRAGAgent 使用外部传入的 ToolCallingChatModel 创建 RAG Agent，避免重复创建模型实例。
func NewRAGAgent(
	chatModel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	ragContextFunc func(context.Context, string) (string, error),
) (*RAGAgent, error) {
	reactAgent, err := react.NewAgent(context.Background(), &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      buildToolsConfig(tools),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return &RAGAgent{
		agent:      reactAgent,
		ragContext: ragContextFunc,
	}, nil
}

func (a *RAGAgent) Run(ctx context.Context, query string) (string, error) {
	ragContext, err := a.ragContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to get RAG context: %w", err)
	}

	enhancedQuery := fmt.Sprintf(`
基于如下参考文档回答用户问题:

【参考文档】
%s

【用户问题】
%s

如参考里没有答案，可以调用可用工具继续检索。`, ragContext, query)

	input := []*schema.Message{
		{
			Role:    schema.User,
			Content: enhancedQuery,
		},
	}

	output, err := a.agent.Generate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("agent generation failed: %w", err)
	}
	if output == nil || output.Content == "" {
		return "", fmt.Errorf("empty response from agent")
	}
	return output.Content, nil
}

// SimpleAgent：兜底纯对话模型。
type SimpleAgent struct {
	model model.ChatModel
}

// NewSimpleAgent 使用外部传入的 ChatModel 创建 SimpleAgent。
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

	cfg := compose.ToolsNodeConfig{
		Tools: make([]tool.BaseTool, len(tools)),
	}
	copy(cfg.Tools, tools)
	return cfg
}
