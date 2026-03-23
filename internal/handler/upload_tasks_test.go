package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestUploadTaskManagerProcessesTask(t *testing.T) {
	var calls atomic.Int64
	mgr := newUploadTaskManager(2, 1, func(ctx context.Context, filePath, metadata string) error {
		calls.Add(1)
		return nil
	})
	defer mgr.Close()

	task, err := mgr.Enqueue("demo.md", "D:/tmp/demo.md", "meta")
	if err != nil {
		t.Fatalf("expected enqueue to succeed, got %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := mgr.Get(task.TaskID)
		if ok && snapshot.State == UploadTaskSucceeded {
			if calls.Load() != 1 {
				t.Fatalf("expected indexer to be called once, got %d", calls.Load())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected upload task to finish")
}

func TestUploadTaskManagerCloseDrainsQueue(t *testing.T) {
	var calls atomic.Int64
	mgr := newUploadTaskManager(2, 1, func(ctx context.Context, filePath, metadata string) error {
		calls.Add(1)
		return nil
	})
	if _, err := mgr.Enqueue("a.md", "a.md", "meta"); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if _, err := mgr.Enqueue("b.md", "b.md", "meta"); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	mgr.Close()
	if calls.Load() != 2 {
		t.Fatalf("expected close to drain queued tasks, got %d", calls.Load())
	}
}
