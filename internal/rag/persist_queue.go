package rag

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type PersistQueueStats struct {
	Enabled      bool   `json:"enabled"`
	QueueSize    int    `json:"queue_size"`
	QueueCap     int    `json:"queue_cap"`
	Enqueued     uint64 `json:"enqueued"`
	Dropped      uint64 `json:"dropped"`
	Failed       uint64 `json:"failed"`
	LastError    string `json:"last_error,omitempty"`
	LastFinished string `json:"last_finished,omitempty"`
}

type persistTask struct {
	query  string
	answer string
}

type persistQueue struct {
	tasks        chan persistTask
	handler      func(context.Context, string, string) error
	timeout      time.Duration
	mu           sync.RWMutex
	closed       bool
	lastError    string
	lastFinished time.Time
	enqueued     atomic.Uint64
	dropped      atomic.Uint64
	failed       atomic.Uint64
	wg           sync.WaitGroup
}

func newPersistQueue(size int, timeout time.Duration, handler func(context.Context, string, string) error) *persistQueue {
	if size <= 0 {
		size = 16
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	queue := &persistQueue{
		tasks:   make(chan persistTask, size),
		handler: handler,
		timeout: timeout,
	}
	queue.wg.Add(1)
	go queue.run()
	return queue
}

func (q *persistQueue) Enqueue(query, answer string) bool {
	q.mu.RLock()
	closed := q.closed
	q.mu.RUnlock()
	if closed {
		q.dropped.Add(1)
		return false
	}

	select {
	case q.tasks <- persistTask{query: query, answer: answer}:
		q.enqueued.Add(1)
		return true
	default:
		q.dropped.Add(1)
		return false
	}
}

func (q *persistQueue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.tasks)
	}
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *persistQueue) Stats() PersistQueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := PersistQueueStats{
		Enabled:   true,
		QueueSize: len(q.tasks),
		QueueCap:  cap(q.tasks),
		Enqueued:  q.enqueued.Load(),
		Dropped:   q.dropped.Load(),
		Failed:    q.failed.Load(),
		LastError: q.lastError,
	}
	if !q.lastFinished.IsZero() {
		stats.LastFinished = q.lastFinished.Format(time.RFC3339)
	}
	return stats
}

func (q *persistQueue) run() {
	defer q.wg.Done()
	for task := range q.tasks {
		ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
		err := q.handler(ctx, task.query, task.answer)
		cancel()

		q.mu.Lock()
		if err != nil {
			q.failed.Add(1)
			q.lastError = err.Error()
		} else {
			q.lastError = ""
			q.lastFinished = time.Now()
		}
		q.mu.Unlock()
	}
}
