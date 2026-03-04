package graph

import (
	"context"
	"fmt"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type RAGGraphConfig struct {
	APIKey       string
	BaseURL      string
	Model        string
	RAGContext   func(context.Context, string) (string, error)
	SystemPrompt string
}

type RAGGraph struct {
	runnable compose.Runnable[map[string]any, *schema.Message]
}

func NewRAGGraph(config *RAGGraphConfig) (*RAGGraph, error) {
	chatModel, err := arkComponent.NewChatModel(context.Background(), &arkComponent.ChatModelConfig{
		APIKey:  config.APIKey,
		BaseURL: config.BaseURL,
		Model:   config.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model: %w", err)
	}

	g := compose.NewGraph[map[string]any, *schema.Message]()

	g.AddLambdaNode("retrieve", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query, ok := input["query"].(string)
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}

		ragContext, err := config.RAGContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve context: %w", err)
		}

		input["context"] = ragContext
		return input, nil
	}))

	g.AddLambdaNode("build_prompt", compose.InvokableLambda(func(ctx context.Context, input map[string]any) ([]*schema.Message, error) {
		query := input["query"].(string)
		context := input["context"].(string)

		systemPrompt := config.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = "你是资深的网络热词助手，必须基于给定的参考资料回答问题。"
		}

		messages := []*schema.Message{
			{
				Role:    schema.System,
				Content: systemPrompt,
			},
			{
				Role: schema.User,
				Content: fmt.Sprintf(`
【参考文档】
%s

【用户问题】
%s

请基于参考内容提供准确、详细的回答。`, context, query),
			},
		}

		return messages, nil
	}))

	g.AddChatModelNode("generate", chatModel)

	g.AddEdge(compose.START, "retrieve")
	g.AddEdge("retrieve", "build_prompt")
	g.AddEdge("build_prompt", "generate")
	g.AddEdge("generate", compose.END)

	runnable, err := g.Compile(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}

	return &RAGGraph{
		runnable: runnable,
	}, nil
}

func (g *RAGGraph) Run(ctx context.Context, query string) (*schema.Message, error) {
	input := map[string]any{"query": query}
	output, err := g.runnable.Invoke(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("graph execution failed: %w", err)
	}
	return output, nil
}

func (g *RAGGraph) Stream(ctx context.Context, query string) (*schema.StreamReader[*schema.Message], error) {
	input := map[string]any{"query": query}
	return g.runnable.Stream(ctx, input)
}

type MultiStageGraphConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	RAGContext func(context.Context, string) (string, error)
	Tools      map[string]func(context.Context, string) (string, error)
}

type MultiStageGraph struct {
	runnable compose.Runnable[map[string]any, map[string]any]
}

func NewMultiStageGraph(config *MultiStageGraphConfig) (*MultiStageGraph, error) {
	chatModel, err := arkComponent.NewChatModel(context.Background(), &arkComponent.ChatModelConfig{
		APIKey:  config.APIKey,
		BaseURL: config.BaseURL,
		Model:   config.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model: %w", err)
	}

	tools := config.Tools
	if tools == nil {
		tools = map[string]func(context.Context, string) (string, error){}
	}

	g := compose.NewGraph[map[string]any, map[string]any]()

	g.AddLambdaNode("intent_recognition", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query := input["query"].(string)
		messages := []*schema.Message{
			{
				Role:    schema.System,
				Content: "分析用户是想要 search、analysis、explanation 还是 comprehensive，请直接返回其中一个英文单词。",
			},
			{
				Role:    schema.User,
				Content: query,
			},
		}

		output, err := chatModel.Generate(ctx, messages)
		if err != nil {
			return nil, err
		}

		input["intent"] = output.Content
		return input, nil
	}))

	g.AddLambdaNode("rag_retrieve", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query := input["query"].(string)
		ragContext, err := config.RAGContext(ctx, query)
		if err != nil {
			return nil, err
		}
		input["rag_context"] = ragContext
		return input, nil
	}))

	g.AddLambdaNode("tool_execution", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		intent, _ := input["intent"].(string)
		query := input["query"].(string)

		if toolFunc, ok := tools[intent]; ok {
			result, err := toolFunc(ctx, query)
			if err != nil {
				return nil, err
			}
			input["tool_result"] = result
		}
		return input, nil
	}))

	g.AddLambdaNode("final_generation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query := input["query"].(string)
		ragContext := input["rag_context"].(string)
		toolResult, _ := input["tool_result"].(string)

		messages := []*schema.Message{
			{
				Role:    schema.System,
				Content: "你是网络热词专家，整合所有上下文给用户提供结论。",
			},
			{
				Role: schema.User,
				Content: fmt.Sprintf(`
【RAG 检索结果】
%s

【工具输出】
%s

【用户问题】
%s

请汇总上述信息给出详细答案。`, ragContext, toolResult, query),
			},
		}

		output, err := chatModel.Generate(ctx, messages)
		if err != nil {
			return nil, err
		}

		input["final_answer"] = output.Content
		return input, nil
	}))

	g.AddEdge(compose.START, "intent_recognition")
	g.AddEdge("intent_recognition", "rag_retrieve")
	g.AddEdge("rag_retrieve", "tool_execution")
	g.AddEdge("tool_execution", "final_generation")
	g.AddEdge("final_generation", compose.END)

	runnable, err := g.Compile(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}

	return &MultiStageGraph{runnable: runnable}, nil
}

func (g *MultiStageGraph) Run(ctx context.Context, query string) (string, error) {
	input := map[string]any{"query": query}

	output, err := g.runnable.Invoke(ctx, input)
	if err != nil {
		return "", fmt.Errorf("graph execution failed: %w", err)
	}

	if finalAnswer, ok := output["final_answer"].(string); ok && finalAnswer != "" {
		return finalAnswer, nil
	}

	return "", fmt.Errorf("no final answer in output")
}
