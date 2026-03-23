package agent

import (
	"context"
	"strings"
)

type DefaultPlanner struct{}

func NewPlanner() *DefaultPlanner {
	return &DefaultPlanner{}
}

func (p *DefaultPlanner) BuildPlan(ctx context.Context, query string) (ExecutionPlan, error) {
	_ = ctx
	complexity := detectComplexity(query)
	goal := "回答用户关于网络热词的问题"
	steps := make([]PlanStep, 0, 4)

	if needsSearchStep(query) {
		steps = append(steps, PlanStep{Name: "search", Required: true, Reason: "需要先获取热词事实或背景信息"})
	}
	if needsAnalysisStep(query) {
		steps = append(steps, PlanStep{Name: "analysis", Required: true, Reason: "问题涉及趋势、原因或影响分析", DependsOn: []string{"search"}})
	}
	if needsExplanationStep(query) {
		steps = append(steps, PlanStep{Name: "explanation", Required: true, Reason: "用户需要含义、起源或使用方式说明", DependsOn: []string{"search"}})
	}
	if len(steps) == 0 {
		steps = append(steps,
			PlanStep{Name: "search", Required: true, Reason: "默认先补齐基础事实"},
			PlanStep{Name: "explanation", Required: true, Reason: "默认提供简明解释", DependsOn: []string{"search"}},
		)
	}
	steps = append(steps, PlanStep{Name: "summarize", Required: true, Reason: "统一整合子步骤结果", DependsOn: requiredStepNames(steps)})

	if complexity == "complex" {
		goal = "对复杂热词问题给出结构化、多视角、基于证据的回答"
	}
	return ExecutionPlan{Goal: goal, Complexity: complexity, Steps: steps}, nil
}

func requiredStepNames(steps []PlanStep) []string {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.Required {
			names = append(names, step.Name)
		}
	}
	return names
}

func needsSearchStep(query string) bool {
	if needsTooling(query) {
		return true
	}
	signals := []string{"是什么", "什么意思", "背景", "起源", "来源", "最近", "热词"}
	for _, signal := range signals {
		if strings.Contains(query, signal) {
			return true
		}
	}
	return true
}

func needsAnalysisStep(query string) bool {
	signals := []string{"为什么", "趋势", "分析", "影响", "原因", "火", "变化"}
	for _, signal := range signals {
		if strings.Contains(query, signal) {
			return true
		}
	}
	return false
}

func needsExplanationStep(query string) bool {
	signals := []string{"什么意思", "是什么", "解释", "怎么用", "起源", "含义", "梗"}
	for _, signal := range signals {
		if strings.Contains(query, signal) {
			return true
		}
	}
	return detectComplexity(query) != "complex"
}
