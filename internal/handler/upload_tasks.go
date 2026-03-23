package handler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type UploadTaskState string

const (
	UploadTaskQueued    UploadTaskState = "queued"
	UploadTaskRunning   UploadTaskState = "running"
	UploadTaskSucceeded UploadTaskState = "succeeded"
	UploadTaskFailed    UploadTaskState = "failed"
)

type UploadTaskSnapshot struct {
	TaskID      string          `json:"task_id"`
	Filename    string          `json:"filename"`
	State       UploadTaskState `json:"state"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   string          `json:"created_at"`
	StartedAt   string          `json:"started_at,omitempty"`
	FinishedAt  string          `json:"finished_at,omitempty"`
	StatusURL   string          `json:"status_url,omitempty"`
	VisibleHint string          `json:"visible_hint,omitempty"`
}

type UploadQueueStats struct {
	Enabled   bool   `json:"enabled"`
	QueueSize int    `json:"queue_size"`
	QueueCap  int    `json:"queue_cap"`
	Workers   int    `json:"workers"`
	Enqueued  uint64 `json:"enqueued"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
}

type uploadTask struct {
	id        string
	filename  string
	filePath  string
	metadata  string
	state     UploadTaskState
	errorText string
	createdAt time.Time
	startedAt time.Time
	endedAt   time.Time
}

type uploadTaskManager struct {
	queue    chan *uploadTask
	indexer  func(context.Context, string, string) error
	workers  int
	mu       sync.RWMutex
	tasks    map[string]*uploadTask
	closed   bool
	counter  atomic.Uint64
	enqueued atomic.Uint64
	complete atomic.Uint64
	failed   atomic.Uint64
	wg       sync.WaitGroup
}

func newUploadTaskManager(queueSize, workers int, indexer func(context.Context, string, string) error) *uploadTaskManager {
	if queueSize <= 0 {
		queueSize = 8
	}
	if workers <= 0 {
		workers = 2
	}
	mgr := &uploadTaskManager{
		queue:   make(chan *uploadTask, queueSize),
		indexer: indexer,
		workers: workers,
		tasks:   make(map[string]*uploadTask),
	}
	for i := 0; i < workers; i++ {
		mgr.wg.Add(1)
		go mgr.worker()
	}
	return mgr
}

func (m *uploadTaskManager) Enqueue(filename, filePath, metadata string) (*UploadTaskSnapshot, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("upload task queue is closed")
	}
	task := &uploadTask{
		id:        fmt.Sprintf("upload-%d", time.Now().UnixNano()+int64(m.counter.Add(1))),
		filename:  filename,
		filePath:  filePath,
		metadata:  metadata,
		state:     UploadTaskQueued,
		createdAt: time.Now(),
	}
	m.tasks[task.id] = task
	m.mu.Unlock()

	select {
	case m.queue <- task:
		m.enqueued.Add(1)
		snapshot := m.snapshot(task)
		return &snapshot, nil
	default:
		m.mu.Lock()
		delete(m.tasks, task.id)
		m.mu.Unlock()
		return nil, fmt.Errorf("upload task queue is full")
	}
}

func (m *uploadTaskManager) Get(taskID string) (UploadTaskSnapshot, bool) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return UploadTaskSnapshot{}, false
	}
	return m.snapshot(task), true
}

func (m *uploadTaskManager) Stats() UploadQueueStats {
	return UploadQueueStats{
		Enabled:   true,
		QueueSize: len(m.queue),
		QueueCap:  cap(m.queue),
		Workers:   m.workers,
		Enqueued:  m.enqueued.Load(),
		Completed: m.complete.Load(),
		Failed:    m.failed.Load(),
	}
}

func (m *uploadTaskManager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.queue)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *uploadTaskManager) worker() {
	defer m.wg.Done()
	for task := range m.queue {
		m.mu.Lock()
		task.state = UploadTaskRunning
		task.startedAt = time.Now()
		m.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		err := m.indexer(ctx, task.filePath, task.metadata)
		cancel()

		m.mu.Lock()
		task.endedAt = time.Now()
		if err != nil {
			task.state = UploadTaskFailed
			task.errorText = err.Error()
			m.failed.Add(1)
		} else {
			task.state = UploadTaskSucceeded
			m.complete.Add(1)
		}
		m.mu.Unlock()
	}
}

func (m *uploadTaskManager) snapshot(task *uploadTask) UploadTaskSnapshot {
	snapshot := UploadTaskSnapshot{
		TaskID:      task.id,
		Filename:    task.filename,
		State:       task.state,
		Error:       task.errorText,
		CreatedAt:   task.createdAt.Format(time.RFC3339),
		VisibleHint: "任务成功后，内容可能还需要短暂等待一次批量 flush 才能被检索到。",
	}
	if !task.startedAt.IsZero() {
		snapshot.StartedAt = task.startedAt.Format(time.RFC3339)
	}
	if !task.endedAt.IsZero() {
		snapshot.FinishedAt = task.endedAt.Format(time.RFC3339)
	}
	return snapshot
}

