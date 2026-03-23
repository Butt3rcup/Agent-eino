package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"

	"go-eino-agent/config"
	"go-eino-agent/internal/agent"
	"go-eino-agent/internal/graph"
	"go-eino-agent/internal/handler"
	"go-eino-agent/internal/rag"
	hotwordTool "go-eino-agent/internal/tool"
	"go-eino-agent/pkg/embedding"
	"go-eino-agent/pkg/logger"
	"go-eino-agent/pkg/vectordb"
)

func Build(ctx context.Context, cfg *config.Config) (handler.Dependencies, error) {
	status := handler.NewRuntimeStatus()
	registerDefaultModes(status)

	router := agent.NewRouter()
	planner := agent.NewPlanner()
	validator := agent.NewValidator()
	fallbackController := agent.NewFallbackController()
	status.SetComponent("agent_router", false, nil)
	status.SetComponent("agent_planner", false, nil)
	status.SetComponent("agent_validator", false, nil)
	status.SetComponent("agent_fallback", false, nil)

	embeddingSvc, err := embedding.NewService(ctx, cfg.Ark.APIKey, cfg.Ark.BaseURL, cfg.Ark.Embedder)
	status.SetComponent("embedding", false, err)
	if err != nil {
		return handler.Dependencies{}, fmt.Errorf("failed to create embedding service: %w", err)
	}

	milvusClient, err := vectordb.NewMilvusClient(ctx, cfg.Milvus.URI, cfg.Milvus.Token, cfg.Milvus.DBName, cfg.Milvus.CollectionName, cfg.RAG.EmbeddingDim)
	status.SetComponent("milvus_client", false, err)
	if err != nil {
		return handler.Dependencies{}, fmt.Errorf("failed to create milvus client: %w", err)
	}
	if err := milvusClient.CreateCollection(ctx); err != nil {
		status.SetComponent("milvus_collection", false, err)
		milvusClient.Close()
		return handler.Dependencies{}, fmt.Errorf("failed to initialize milvus collection: %w", err)
	}
	status.SetComponent("milvus_collection", false, nil)

	baseChatModel, toolChatModel, err := buildChatModel(ctx, cfg)
	status.SetComponent("chat_model", false, err)
	if err != nil {
		milvusClient.Close()
		return handler.Dependencies{}, fmt.Errorf("failed to create chat model: %w", err)
	}

	var webSearchTool *hotwordTool.VolcanoWebSearchTool
	webSearchTool, err = hotwordTool.NewVolcanoWebSearchTool(ctx, cfg.Ark.APIKey, cfg.Ark.BaseURL, cfg.Ark.Model)
	status.SetComponent("web_search", true, err)
	if err != nil {
		logger.Warn("optional web search is unavailable", zap.Error(err))
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
		webSearchTool,
		cfg.RAG.EnableAutoSearch,
		cfg.RAG.SimilarityThreshold,
		cfg.RAG.AutoSaveSearchResult,
		cfg.RAG.AutoSaveMinChars,
		cfg.Upload.Dir,
		cfg.RAG.AsyncKnowledgePersist,
		cfg.RAG.PersistQueueSize,
		cfg.RAG.QueryCacheSize,
		time.Duration(cfg.RAG.QueryCacheTTLSeconds)*time.Second,
	)
	status.SetComponent("rag_service", false, nil)

	deps := handler.Dependencies{
		Config:        cfg,
		RAGService:    ragService,
		ChatModel:     baseChatModel,
		MilvusClient:  milvusClient,
		Router:        router,
		Planner:       planner,
		Validator:     validator,
		Fallback:      fallbackController,
		RuntimeStatus: status,
	}

	if toolChatModel == nil {
		markOptionalModeDown(status, "react", "tool calling chat model unavailable")
		markOptionalModeDown(status, "rag_agent", "tool calling chat model unavailable")
		markOptionalModeDown(status, "multi-agent", "tool calling chat model unavailable")
		markOptionalModeDown(status, "graph_rag", "graph dependencies unavailable")
		markOptionalModeDown(status, "graph_multi", "graph dependencies unavailable")
		return deps, nil
	}

	toolset := []tool.BaseTool{
		hotwordTool.NewHotwordSearchTool(milvusClient, embeddingSvc),
		hotwordTool.NewTrendAnalysisTool(),
		hotwordTool.NewExplanationTool(),
	}
	if webSearchTool != nil {
		toolset = append(toolset, webSearchTool)
	}

	if reactAgent, reactErr := agent.NewReActAgent(ctx, toolChatModel, agent.ModeReact, toolset, validator); reactErr != nil {
		markOptionalModeDown(status, "react", reactErr.Error())
	} else {
		deps.ReactAgent = reactAgent
		status.SetMode("react", true, "")
	}

	if ragAgent, ragErr := agent.NewRAGAgent(ctx, toolChatModel, agent.ModeRAGAgent, toolset, ragService.BuildContext, validator); ragErr != nil {
		markOptionalModeDown(status, "rag_agent", ragErr.Error())
	} else {
		deps.RAGAgent = ragAgent
		status.SetMode("rag_agent", true, "")
	}

	if multiAgent, multiErr := agent.NewMultiAgentSystem(ctx, toolChatModel, milvusClient, embeddingSvc, webSearchTool, planner, validator); multiErr != nil {
		markOptionalModeDown(status, "multi-agent", multiErr.Error())
	} else {
		deps.MultiAgent = multiAgent
		status.SetMode("multi-agent", true, "")
	}

	if ragGraph, graphErr := graph.NewRAGGraph(ctx, &graph.RAGGraphConfig{
		ChatModel:    baseChatModel,
		APIKey:       cfg.Ark.APIKey,
		BaseURL:      cfg.Ark.BaseURL,
		Model:        cfg.Ark.Model,
		RAGContext:   ragService.BuildContext,
		SystemPrompt: "你是资深网络热词顾问，必须引用参考资料作答。",
		Validator:    validator,
	}); graphErr != nil {
		markOptionalModeDown(status, "graph_rag", graphErr.Error())
	} else {
		deps.RAGGraph = ragGraph
		status.SetMode("graph_rag", true, "")
	}

	graphToolHandlers := map[string]func(context.Context, string) (string, error){}
	if deps.MultiAgent != nil {
		graphToolHandlers = map[string]func(context.Context, string) (string, error){
			"search": func(ctx context.Context, query string) (string, error) {
				return deps.MultiAgent.ProcessQuery(ctx, query, "search")
			},
			"analysis": func(ctx context.Context, query string) (string, error) {
				return deps.MultiAgent.ProcessQuery(ctx, query, "analysis")
			},
			"explanation": func(ctx context.Context, query string) (string, error) {
				return deps.MultiAgent.ProcessQuery(ctx, query, "explanation")
			},
		}
	}

	if multiGraph, graphErr := graph.NewMultiStageGraph(ctx, &graph.MultiStageGraphConfig{
		ChatModel:  baseChatModel,
		APIKey:     cfg.Ark.APIKey,
		BaseURL:    cfg.Ark.BaseURL,
		Model:      cfg.Ark.Model,
		RAGContext: ragService.BuildContext,
		Tools:      graphToolHandlers,
		Planner:    planner,
		Validator:  validator,
	}); graphErr != nil {
		markOptionalModeDown(status, "graph_multi", graphErr.Error())
	} else {
		deps.MultiGraph = multiGraph
		status.SetMode("graph_multi", true, "")
	}

	return deps, nil
}

func buildChatModel(ctx context.Context, cfg *config.Config) (model.ChatModel, model.ToolCallingChatModel, error) {
	if cfg.Ollama.BaseURL != "" && cfg.Ollama.Model != "" {
		ollamaModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{BaseURL: cfg.Ollama.BaseURL, Model: cfg.Ollama.Model})
		if err != nil {
			return nil, nil, err
		}
		logger.Info("using local Ollama model", zap.String("baseURL", cfg.Ollama.BaseURL), zap.String("model", cfg.Ollama.Model))
		return ollamaModel, ollamaModel, nil
	}

	arkModel, err := arkComponent.NewChatModel(ctx, &arkComponent.ChatModelConfig{APIKey: cfg.Ark.APIKey, BaseURL: cfg.Ark.BaseURL, Model: cfg.Ark.Model})
	if err != nil {
		return nil, nil, err
	}
	logger.Info("using Ark model", zap.String("baseURL", cfg.Ark.BaseURL), zap.String("model", cfg.Ark.Model))
	return arkModel, arkModel, nil
}

func registerDefaultModes(status *handler.RuntimeStatus) {
	status.SetMode("rag", true, "")
	status.SetMode("search", true, "")
	status.SetMode("upload", true, "")
	status.SetMode("react", false, "not initialized yet")
	status.SetMode("rag_agent", false, "not initialized yet")
	status.SetMode("multi-agent", false, "not initialized yet")
	status.SetMode("graph_rag", false, "not initialized yet")
	status.SetMode("graph_multi", false, "not initialized yet")
}

func markOptionalModeDown(status *handler.RuntimeStatus, mode, reason string) {
	status.SetComponent(mode, true, errors.New(reason))
	status.SetMode(mode, false, reason)
	logger.Warn("optional mode unavailable", zap.String("mode", mode), zap.String("reason", reason))
}
