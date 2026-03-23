package agent

import (
	"context"
	"testing"
)

func TestPlannerBuildsTrendPlan(t *testing.T) {
	planner := NewPlanner()
	plan, err := planner.BuildPlan(context.Background(), "这个热词为什么最近突然火了？")
	if err != nil {
		t.Fatalf("expected planner to succeed, got %v", err)
	}
	if plan.Complexity == "simple" {
		t.Fatalf("expected non-simple plan, got %s", plan.Complexity)
	}
	if !containsStep(plan.Steps, "search") {
		t.Fatal("expected search step")
	}
	if !containsStep(plan.Steps, "analysis") {
		t.Fatal("expected analysis step")
	}
	if !containsStep(plan.Steps, "summarize") {
		t.Fatal("expected summarize step")
	}
}

func containsStep(steps []PlanStep, name string) bool {
	for _, step := range steps {
		if step.Name == name && step.Required {
			return true
		}
	}
	return false
}
