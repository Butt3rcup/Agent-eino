package handler

import (
	"context"
	"fmt"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
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
		cfg.Milvus.DBName,
		cfg.Milvus.CollectionName,
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
		cfg.RAG.AutoSaveMinChars,
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
