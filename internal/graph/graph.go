package graph

import (
	"context"
	"fmt"
	"strings"
	"sync"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/sync/errgroup"

	"go-eino-agent/internal/agent"
)

type RAGGraphConfig struct {
	ChatModel model.ChatModel
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	APIKey string
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	BaseURL string
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	Model        string
	RAGContext   func(context.Context, string) (string, error)
	SystemPrompt string
	Validator    agent.Validator
}

type RAGGraph struct {
	runnable   compose.Runnable[map[string]any, *schema.Message]
	ragContext func(context.Context, string) (string, error)
	validator  agent.Validator
}

func NewRAGGraph(ctx context.Context, config *RAGGraphConfig) (*RAGGraph, error) {
	chatModel := config.ChatModel
	if chatModel == nil {
		modelInstance, err := arkComponent.NewChatModel(ctx, &arkComponent.ChatModelConfig{
			APIKey:  config.APIKey,
			BaseURL: config.BaseURL,
			Model:   config.Model,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create chat model: %w", err)
		}
		chatModel = modelInstance
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
		query, ok := getStringInput(input, "query")
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}
		ragContext, ok := getStringInput(input, "context")
		if !ok {
			return nil, fmt.Errorf("context not found in input")
		}

		systemPrompt := config.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = agent.GroundedAnswerPrompt
		}

		return []*schema.Message{
			{Role: schema.System, Content: systemPrompt},
			{Role: schema.User, Content: fmt.Sprintf("【参考文档】%s\n\n【用户问题】%s\n\n请基于参考内容提供准确、详细的回答。", ragContext, query)},
		}, nil
	}))

	g.AddChatModelNode("generate", chatModel)
	g.AddEdge(compose.START, "retrieve")
	g.AddEdge("retrieve", "build_prompt")
	g.AddEdge("build_prompt", "generate")
	g.AddEdge("generate", compose.END)

	runnable, err := g.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}

	return &RAGGraph{runnable: runnable, ragContext: config.RAGContext, validator: config.Validator}, nil
}

func (g *RAGGraph) Run(ctx context.Context, query string) (*schema.Message, error) {
	message, err := g.runnable.Invoke(ctx, map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, fmt.Errorf("empty graph response")
	}
	if g.validator != nil && g.ragContext != nil {
		evidence, ctxErr := g.ragContext(ctx, query)
		if ctxErr == nil {
			if err := g.validator.ValidateAnswer(query, message.Content, evidence); err != nil {
				if trace := agent.TraceFromContext(ctx); trace != nil {
					trace.IncValidationFailures()
				}
				return nil, err
			}
		}
	}
	return message, nil
}

func (g *RAGGraph) Stream(ctx context.Context, query string) (*schema.StreamReader[*schema.Message], error) {
	return g.runnable.Stream(ctx, map[string]any{"query": query})
}

type MultiStageGraphConfig struct {
	ChatModel model.ChatModel
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	APIKey string
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	BaseURL string
	// Deprecated: Rely on ChatModel instead of Ark credentials.
	Model      string
	RAGContext func(context.Context, string) (string, error)
	Tools      map[string]func(context.Context, string) (string, error)
	Planner    agent.Planner
	Validator  agent.Validator
}

type MultiStageGraph struct {
	runnable  compose.Runnable[map[string]any, map[string]any]
	planner   agent.Planner
	validator agent.Validator
}

func NewMultiStageGraph(ctx context.Context, config *MultiStageGraphConfig) (*MultiStageGraph, error) {
	chatModel := config.ChatModel
	if chatModel == nil {
		modelInstance, err := arkComponent.NewChatModel(ctx, &arkComponent.ChatModelConfig{
			APIKey:  config.APIKey,
			BaseURL: config.BaseURL,
			Model:   config.Model,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create chat model: %w", err)
		}
		chatModel = modelInstance
	}

	tools := config.Tools
	if tools == nil {
		tools = map[string]func(context.Context, string) (string, error){}
	}
	planner := config.Planner
	if planner == nil {
		planner = agent.NewPlanner()
	}

	g := compose.NewGraph[map[string]any, map[string]any]()
	g.AddLambdaNode("plan_generation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query, ok := getStringInput(input, "query")
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}
		plan, err := planner.BuildPlan(ctx, query)
		if err != nil {
			return nil, err
		}
		if trace := agent.TraceFromContext(ctx); trace != nil {
			trace.MarkPlannerUsed()
		}
		input["plan"] = plan
		return input, nil
	}))

	g.AddLambdaNode("rag_retrieve", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query, ok := getStringInput(input, "query")
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}
		ragContext, err := config.RAGContext(ctx, query)
		if err != nil {
			return nil, err
		}
		input["rag_context"] = ragContext
		return input, nil
	}))

	g.AddLambdaNode("conditional_tool_execution", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query, ok := getStringInput(input, "query")
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}
		plan, ok := input["plan"].(agent.ExecutionPlan)
		if !ok {
			return nil, fmt.Errorf("plan not found in input")
		}
		stepResults := make(map[string]string)
		var mu sync.Mutex
		eg, egCtx := errgroup.WithContext(ctx)
		for _, step := range plan.Steps {
			if !step.Required || step.Name == "summarize" {
				continue
			}
			toolFunc, exists := tools[step.Name]
			if !exists {
				continue
			}
			currentStep := step
			currentToolFunc := toolFunc
			eg.Go(func() error {
				result, err := currentToolFunc(egCtx, query)
				if err != nil {
					return err
				}
				if config.Validator != nil {
					if err := config.Validator.ValidateToolResult(query, currentStep.Name, result); err != nil {
						if trace := agent.TraceFromContext(egCtx); trace != nil {
							trace.IncValidationFailures()
						}
						return err
					}
				}
				mu.Lock()
				stepResults[currentStep.Name] = result
				mu.Unlock()
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return nil, err
		}
		input["step_results"] = stepResults
		return input, nil
	}))

	g.AddLambdaNode("evidence_validation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		ragContext, _ := input["rag_context"].(string)
		stepResults, _ := input["step_results"].(map[string]string)
		evidence := strings.TrimSpace(ragContext)
		for _, value := range stepResults {
			if strings.TrimSpace(value) == "" {
				continue
			}
			if evidence != "" {
				evidence += "\n\n"
			}
			evidence += value
		}
		if evidence == "" {
			return nil, fmt.Errorf("no evidence available for final generation")
		}
		input["evidence"] = evidence
		return input, nil
	}))

	g.AddLambdaNode("final_generation", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		query, ok := getStringInput(input, "query")
		if !ok {
			return nil, fmt.Errorf("query not found in input")
		}
		evidence, ok := getStringInput(input, "evidence")
		if !ok {
			return nil, fmt.Errorf("evidence not found in input")
		}
		output, err := chatModel.Generate(ctx, []*schema.Message{
			{Role: schema.System, Content: agent.GroundedAnswerPrompt},
			{Role: schema.User, Content: fmt.Sprintf("【执行计划】%v\n\n【综合证据】%s\n\n【用户问题】%s\n\n请基于证据输出结构化、准确的答案。", input["plan"], evidence, query)},
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, fmt.Errorf("final generation returned empty output")
		}
		if config.Validator != nil {
			if err := config.Validator.ValidateAnswer(query, output.Content, evidence); err != nil {
				if trace := agent.TraceFromContext(ctx); trace != nil {
					trace.IncValidationFailures()
				}
				return nil, err
			}
		}
		input["final_answer"] = output.Content
		return input, nil
	}))

	g.AddEdge(compose.START, "plan_generation")
	g.AddEdge("plan_generation", "rag_retrieve")
	g.AddEdge("rag_retrieve", "conditional_tool_execution")
	g.AddEdge("conditional_tool_execution", "evidence_validation")
	g.AddEdge("evidence_validation", "final_generation")
	g.AddEdge("final_generation", compose.END)

	runnable, err := g.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}

	return &MultiStageGraph{runnable: runnable, planner: planner, validator: config.Validator}, nil
}

func (g *MultiStageGraph) Run(ctx context.Context, query string) (string, error) {
	output, err := g.runnable.Invoke(ctx, map[string]any{"query": query})
	if err != nil {
		return "", fmt.Errorf("graph execution failed: %w", err)
	}
	if finalAnswer, ok := output["final_answer"].(string); ok && finalAnswer != "" {
		return finalAnswer, nil
	}
	return "", fmt.Errorf("no final answer in output")
}

func getStringInput(input map[string]any, key string) (string, bool) {
	v, ok := input[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
