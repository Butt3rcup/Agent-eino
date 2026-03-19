package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"go-eino-agent/pkg/logger"
)

var toolCallDescriptionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`用户.*调用.*函数`),
	regexp.MustCompile(`调用.*函数.*获取`),
	regexp.MustCompile(`使用.*工具.*获取`),
	regexp.MustCompile(`想了解.*调用.*函数`),
}

func (h *Handler) streamMessageReader(c *gin.Context, flusher http.Flusher, reader *schema.StreamReader[*schema.Message]) {
	defer reader.Close()

	toolCallsShown := make(map[string]bool)
	buffer := ""
	flushThreshold := 50
	chunkCount := 0

	for {
		chunk, err := reader.Recv()
		chunkCount++

		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Info("[StreamReader] 流结束", zap.Int("totalChunks", chunkCount))
				if buffer != "" {
					h.writeSSEMessage(c, flusher, buffer)
				}
				h.writeSSEDone(c, flusher)
				break
			}
			h.writeSSEError(c, flusher, fmt.Sprintf("读取流失败: %v", err))
			break
		}

		if chunk == nil {
			continue
		}

		logger.Debug("[StreamReader] 收到chunk",
			zap.Int("chunkNum", chunkCount),
			zap.String("role", string(chunk.Role)),
			zap.Int("contentLen", len(chunk.Content)),
			zap.Int("toolCallsCount", len(chunk.ToolCalls)),
		)

		if len(chunk.ToolCalls) > 0 {
			if buffer != "" && isToolCallDescription(buffer) {
				logger.Debug("[StreamReader] 丢弃工具调用描述", zap.String("buffer", buffer))
				buffer = ""
			} else if buffer != "" {
				logger.Debug("[StreamReader] 输出缓存内容", zap.String("buffer", buffer))
				h.writeSSEMessage(c, flusher, buffer)
				buffer = ""
			}

			for _, toolCall := range chunk.ToolCalls {
				toolID := toolCall.ID
				toolName := toolCall.Function.Name
				if toolID != "" && toolName != "" && strings.Contains(toolName, "_") && !toolCallsShown[toolID] {
					toolInfo := fmt.Sprintf("\n\n🔧 正在调用工具: %s ...\n\n", toolName)
					h.writeSSEMessage(c, flusher, toolInfo)
					toolCallsShown[toolID] = true
					logger.Info("[StreamReader] 显示工具调用", zap.String("tool", toolName))
				}
			}
			continue
		}

		if chunk.Content == "" {
			continue
		}

		buffer += chunk.Content
		if strings.Contains(buffer, "。") {
			if isToolCallDescription(buffer) {
				logger.Debug("[StreamReader] 检测到工具调用描述，丢弃", zap.String("buffer", buffer))
				buffer = ""
			} else {
				h.writeSSEMessage(c, flusher, buffer)
				buffer = ""
			}
			continue
		}

		if len(buffer) > flushThreshold && !strings.Contains(buffer, "调用") && !strings.Contains(buffer, "函数") {
			h.writeSSEMessage(c, flusher, buffer)
			buffer = ""
		}
	}
}

func isToolCallDescription(text string) bool {
	for _, pattern := range toolCallDescriptionPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func (h *Handler) streamTextResult(c *gin.Context, flusher http.Flusher, fn func(context.Context) (string, error)) {
	result, err := fn(c.Request.Context())
	if err != nil {
		h.writeSSEError(c, flusher, err.Error())
		return
	}

	h.writeSSEMessage(c, flusher, result)
	h.writeSSEDone(c, flusher)
}

func prepareSSE(c *gin.Context) (http.Flusher, bool) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "当前响应不支持 SSE"})
		return nil, false
	}
	return flusher, true
}

func (h *Handler) writeSSEMessage(c *gin.Context, flusher http.Flusher, content string) {
	c.SSEvent("message", gin.H{"content": content})
	flusher.Flush()
}

func (h *Handler) writeSSEError(c *gin.Context, flusher http.Flusher, message string) {
	c.SSEvent("error", gin.H{"message": message})
	flusher.Flush()
}

func (h *Handler) writeSSEDone(c *gin.Context, flusher http.Flusher) {
	c.SSEvent("done", gin.H{"message": "完成"})
	flusher.Flush()
}
