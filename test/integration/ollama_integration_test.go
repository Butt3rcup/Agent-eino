package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/schema"
)

// TestOllamaIntegration 是一个简单的终端集成测试
// 此测试会连接到本地的 Ollama 实例（http://127.0.0.1:11434），调用大模型 qwen3:0.6b 进行简单的问答测试。
func TestOllamaIntegration(t *testing.T) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("RUN_OLLAMA_INTEGRATION"))) != "true" {
		t.Skip("未设置 RUN_OLLAMA_INTEGRATION=true，跳过本地 Ollama 集成测试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	baseURL := "http://127.0.0.1:11434"
	modelName := "qwen3:0.6b"

	t.Logf("正在尝试连接到本地 Ollama 模型: %s %s", baseURL, modelName)

	chatModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
	})
	if err != nil {
		t.Fatalf("无法创建 Ollama ChatModel 实例: %v", err)
	}

	messages := []*schema.Message{
		{
			Role:    schema.System,
			Content: "如果你能收到这条消息，请回复：'系统正常工作'。",
		},
		{
			Role:    schema.User,
			Content: "您好，准备好测试了吗？",
		},
	}

	t.Logf("发送测试消息...")
	resp, err := chatModel.Generate(ctx, messages)
	if err != nil {
		// 这里如果不匹配或服务未启动可能会报错连接拒绝或者模型不存在
		// 我们判断一下错误原因来给用户更清晰的提示
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "connectex: No connection could be made") {
			t.Skipf("本地没有启动 Ollama 服务，跳过集成测试：%v", err)
		} else if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			t.Skipf("本地 Ollama 服务响应超时，跳过集成测试：%v", err)
		} else if strings.Contains(err.Error(), "model") && strings.Contains(err.Error(), "not found") {
			t.Skipf("未找到指定的模型 %s，请执行 `ollama run %s` 提前下载模型，错误：%v", modelName, modelName, err)
		}
		t.Fatalf("调用 Ollama 模型时发生错误: %v", err)
	}

	t.Logf("收到本地 Ollama 成功回复: \n%s", resp.Content)
	fmt.Printf("收到本地 Ollama 成功回复: \n%s\n", resp.Content)
}
