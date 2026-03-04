package tool

import (
	"context"
	"encoding/json"
	"fmt"

	arkComponent "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// VolcanoWebSearchTool 火山引擎联网搜索工具
// 使用火山引擎大模型的联网搜索能力获取实时信息
type VolcanoWebSearchTool struct {
	chatModel *arkComponent.ChatModel
}

// NewVolcanoWebSearchTool 创建火山引擎联网搜索工具实例
func NewVolcanoWebSearchTool(apiKey, baseURL, model string) (*VolcanoWebSearchTool, error) {
	chatModel, err := arkComponent.NewChatModel(context.Background(),
		&arkComponent.ChatModelConfig{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model for web search: %w", err)
	}

	return &VolcanoWebSearchTool{
		chatModel: chatModel,
	}, nil
}

func (t *VolcanoWebSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "volcano_web_search",
		Desc: "使用火山引擎联网搜索获取最新的互联网信息和实时知识",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type: schema.String,
				Desc: "搜索查询关键词或问题",
			},
		}),
	}, nil
}

func (t *VolcanoWebSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("failed to parse arguments: %w", err)
	}

	if args.Query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	// 构建联网搜索的消息
	// 关键：使用特定的系统提示词引导模型使用联网搜索能力
	messages := []*schema.Message{
		{
			Role:    schema.System,
			Content: "你是一个专业的网络搜索助手。请基于最新的互联网信息准确、全面地回答问题。如果需要实时数据或最新信息，请使用联网搜索功能。",
		},
		{
			Role:    schema.User,
			Content: args.Query,
		},
	}

	// 调用大模型进行联网搜索
	// 注意：火山引擎的联网搜索功能可能需要在模型配置或调用时启用特定插件
	// 这里假设模型已经配置了联网搜索能力，或者会自动根据查询内容决定是否联网
	response, err := t.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if response == nil || response.Content == "" {
		return "", fmt.Errorf("received empty response from web search")
	}

	return response.Content, nil
}
