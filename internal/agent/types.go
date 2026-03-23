package agent

import (
	"context"
	"sync"
)

const (
	ModeRAG        = "rag"
	ModeReact      = "react"
	ModeRAGAgent   = "rag_agent"
	ModeMultiAgent = "multi-agent"
	ModeGraphRAG   = "graph_rag"
	ModeGraphMulti = "graph_multi"
)

type AgentRoute struct {
	RequestedMode string
	ResolvedMode  string
	Reason        string
	NeedTools     bool
	NeedRAG       bool
	NeedPlanning  bool
	Complexity    string
}

type PlanStep struct {
	Name      string
	Required  bool
	Reason    string
	DependsOn []string
}

type ExecutionPlan struct {
	Goal       string
	Complexity string
	Steps      []PlanStep
}

type Router interface {
	Resolve(ctx context.Context, query, requestedMode string) (AgentRoute, error)
}

type Planner interface {
	BuildPlan(ctx context.Context, query string) (ExecutionPlan, error)
}

type Validator interface {
	AllowedTools(mode string) map[string]struct{}
	ValidateToolCall(mode, toolName, argumentsInJSON string) error
	ValidateToolResult(query, toolName, result string) error
	ValidateAnswer(query, answer, evidence string) error
}

type traceContextKey struct{}

type RequestTrace struct {
	mu                 sync.Mutex
	RequestedMode      string
	ResolvedMode       string
	RouteReason        string
	FallbackUsed       bool
	FallbackFrom       string
	FallbackTo         string
	PlannerUsed        bool
	ToolCalls          int
	ToolFailures       int
	ValidationFailures int
}

type TraceSnapshot struct {
	RequestedMode      string
	ResolvedMode       string
	RouteReason        string
	FallbackUsed       bool
	FallbackFrom       string
	FallbackTo         string
	PlannerUsed        bool
	ToolCalls          int
	ToolFailures       int
	ValidationFailures int
}

func NewRequestTrace(requestedMode string) *RequestTrace {
	return &RequestTrace{RequestedMode: requestedMode}
}

func WithRequestTrace(ctx context.Context, trace *RequestTrace) context.Context {
	if trace == nil {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey{}, trace)
}

func TraceFromContext(ctx context.Context) *RequestTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(traceContextKey{}).(*RequestTrace)
	return trace
}

func (t *RequestTrace) SetRoute(resolvedMode, reason string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ResolvedMode = resolvedMode
	t.RouteReason = reason
	t.mu.Unlock()
}

func (t *RequestTrace) MarkFallback(from, to string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.FallbackUsed = true
	t.FallbackFrom = from
	t.FallbackTo = to
	t.ResolvedMode = to
	t.mu.Unlock()
}

func (t *RequestTrace) MarkPlannerUsed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.PlannerUsed = true
	t.mu.Unlock()
}

func (t *RequestTrace) IncToolCalls() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ToolCalls++
	t.mu.Unlock()
}

func (t *RequestTrace) IncToolFailures() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ToolFailures++
	t.mu.Unlock()
}

func (t *RequestTrace) IncValidationFailures() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ValidationFailures++
	t.mu.Unlock()
}

func (t *RequestTrace) Snapshot() TraceSnapshot {
	if t == nil {
		return TraceSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return TraceSnapshot{
		RequestedMode:      t.RequestedMode,
		ResolvedMode:       t.ResolvedMode,
		RouteReason:        t.RouteReason,
		FallbackUsed:       t.FallbackUsed,
		FallbackFrom:       t.FallbackFrom,
		FallbackTo:         t.FallbackTo,
		PlannerUsed:        t.PlannerUsed,
		ToolCalls:          t.ToolCalls,
		ToolFailures:       t.ToolFailures,
		ValidationFailures: t.ValidationFailures,
	}
}
