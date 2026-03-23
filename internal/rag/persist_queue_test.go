package rag

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPersistQueueDrainsOnClose(t *testing.T) {
	var processed atomic.Int64
	queue := newPersistQueue(2, time.Second, func(ctx context.Context, query, answer string) error {
		processed.Add(1)
		return nil
	})

	if !queue.Enqueue("q1", "a1") {
		t.Fatal("expected first enqueue to succeed")
	}
	if !queue.Enqueue("q2", "a2") {
		t.Fatal("expected second enqueue to succeed")
	}

	queue.Close()
	if processed.Load() != 2 {
		t.Fatalf("expected queued tasks to drain on close, got %d", processed.Load())
	}
}
