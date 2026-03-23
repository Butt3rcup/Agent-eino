package agent

import (
	"context"
	"fmt"

	baseTool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GuardedTool struct {
	name      string
	mode      string
	base      baseTool.InvokableTool
	validator Validator
}

func NewGuardedTool(ctx context.Context, mode string, validator Validator, base baseTool.BaseTool) (*GuardedTool, error) {
	invokable, ok := base.(baseTool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("tool does not implement InvokableTool")
	}
	info, err := base.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect tool info: %w", err)
	}
	return &GuardedTool{name: info.Name, mode: mode, base: invokable, validator: validator}, nil
}

func (t *GuardedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

func (t *GuardedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...baseTool.Option) (string, error) {
	trace := TraceFromContext(ctx)
	if trace != nil {
		trace.IncToolCalls()
	}
	if t.validator != nil {
		if err := t.validator.ValidateToolCall(t.mode, t.name, argumentsInJSON); err != nil {
			if trace != nil {
				trace.IncToolFailures()
				trace.IncValidationFailures()
			}
			return "", err
		}
	}
	result, err := t.base.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		if trace != nil {
			trace.IncToolFailures()
		}
		return "", err
	}
	if t.validator != nil {
		if err := t.validator.ValidateToolResult("", t.name, result); err != nil {
			if trace != nil {
				trace.IncToolFailures()
				trace.IncValidationFailures()
			}
			return "", err
		}
	}
	return result, nil
}

func GuardTools(ctx context.Context, mode string, validator Validator, tools []baseTool.BaseTool) ([]baseTool.BaseTool, error) {
	if validator == nil || len(tools) == 0 {
		return tools, nil
	}
	guarded := make([]baseTool.BaseTool, 0, len(tools))
	for _, current := range tools {
		wrapped, err := NewGuardedTool(ctx, mode, validator, current)
		if err != nil {
			return nil, err
		}
		guarded = append(guarded, wrapped)
	}
	return guarded, nil
}
