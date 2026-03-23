package handler

import (
	"fmt"

	"github.com/cloudwego/eino/components/model"

	"go-eino-agent/config"
	"go-eino-agent/internal/agent"
	"go-eino-agent/internal/graph"
	"go-eino-agent/internal/rag"
	"go-eino-agent/pkg/vectordb"
)

type Handler struct {
	cfg           *config.Config
	ragService    *rag.Service
	chatModel     model.ChatModel
	milvusClient  *vectordb.MilvusClient
	router        agent.Router
	planner       agent.Planner
	validator     agent.Validator
	fallback      *agent.FallbackController
	uploadTasks   *uploadTaskManager
	reactAgent    *agent.ReActAgent
	multiAgent    *agent.MultiAgentSystem
	ragAgent      *agent.RAGAgent
	ragGraph      *graph.RAGGraph
	multiGraph    *graph.MultiStageGraph
	runtimeStatus *RuntimeStatus
	queryPolicies map[string]queryModePolicy
}

func NewHandler(deps Dependencies) (*Handler, error) {
	if err := deps.Validate(); err != nil {
		return nil, fmt.Errorf("invalid handler dependencies: %w", err)
	}

	return &Handler{
		cfg:           deps.Config,
		ragService:    deps.RAGService,
		chatModel:     deps.ChatModel,
		milvusClient:  deps.MilvusClient,
		router:        deps.Router,
		planner:       deps.Planner,
		validator:     deps.Validator,
		fallback:      deps.Fallback,
		uploadTasks:   newUploadTaskManager(deps.Config.Upload.TaskQueueSize, deps.Config.Upload.TaskWorkers, deps.RAGService.IndexFile),
		reactAgent:    deps.ReactAgent,
		multiAgent:    deps.MultiAgent,
		ragAgent:      deps.RAGAgent,
		ragGraph:      deps.RAGGraph,
		multiGraph:    deps.MultiGraph,
		runtimeStatus: deps.RuntimeStatus,
		queryPolicies: buildQueryModePolicies(deps.Config),
	}, nil
}

func (h *Handler) Close() {
	if h.uploadTasks != nil {
		h.uploadTasks.Close()
	}
	if h.ragService != nil {
		h.ragService.Close()
	}
	if h.milvusClient != nil {
		h.milvusClient.Close()
	}
}
