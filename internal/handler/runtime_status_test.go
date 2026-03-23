package handler

import (
	"fmt"
	"testing"
)

func TestRuntimeStatusSnapshotMarksOptionalFailuresAsDegraded(t *testing.T) {
	status := NewRuntimeStatus()
	status.SetComponent("rag_service", false, nil)
	status.SetComponent("graph_rag", true, fmt.Errorf("graph init failed"))
	status.SetMode("rag", true, "")
	status.SetMode("graph_rag", false, "graph init failed")

	snapshot := status.Snapshot(nil)
	if !snapshot.Ready {
		t.Fatal("expected optional component failure to keep service ready")
	}
	if snapshot.Status != "degraded" {
		t.Fatalf("expected degraded status, got %q", snapshot.Status)
	}
	if len(snapshot.DegradedComponents) != 1 || snapshot.DegradedComponents[0] != "graph_rag" {
		t.Fatalf("unexpected degraded components: %#v", snapshot.DegradedComponents)
	}
}
