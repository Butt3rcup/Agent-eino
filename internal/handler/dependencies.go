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

type Dependencies struct {
	Config        *config.Config
	RAGService    *rag.Service
	ChatModel     model.ChatModel
	MilvusClient  *vectordb.MilvusClient
	Router        agent.Router
	Planner       agent.Planner
	Validator     agent.Validator
	Fallback      *agent.FallbackController
	ReactAgent    *agent.ReActAgent
	MultiAgent    *agent.MultiAgentSystem
	RAGAgent      *agent.RAGAgent
	RAGGraph      *graph.RAGGraph
	MultiGraph    *graph.MultiStageGraph
	RuntimeStatus *RuntimeStatus
}

func (d Dependencies) Validate() error {
	if d.Config == nil {
		return fmt.Errorf("config is required")
	}
	if d.RAGService == nil {
		return fmt.Errorf("rag service is required")
	}
	if d.ChatModel == nil {
		return fmt.Errorf("chat model is required")
	}
	if d.MilvusClient == nil {
		return fmt.Errorf("milvus client is required")
	}
	if d.Router == nil {
		return fmt.Errorf("router is required")
	}
	if d.Planner == nil {
		return fmt.Errorf("planner is required")
	}
	if d.Validator == nil {
		return fmt.Errorf("validator is required")
	}
	if d.Fallback == nil {
		return fmt.Errorf("fallback controller is required")
	}
	if d.RuntimeStatus == nil {
		return fmt.Errorf("runtime status is required")
	}
	return nil
}
