package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrQueueFull    = errors.New("session queue is full")
	ErrQueueTimeout = errors.New("timed out waiting in queue")
)

// QueueEntry represents a pending session request.
type QueueEntry struct {
	Request  CreateSessionRequest
	ResultCh chan QueueResult
	EnqueuedAt time.Time
}

// QueueResult is returned when a queued request is resolved.
type QueueResult struct {
	Session *Session
	Err     error
}

// Queue manages pending session requests when the pool is exhausted.
type Queue struct {
	mu       sync.Mutex
	entries  []*QueueEntry
	maxSize  int
	timeout  time.Duration
}

// NewQueue creates a new session queue.
func NewQueue(maxSize int, timeout time.Duration) *Queue {
	return &Queue{
		maxSize: maxSize,
		timeout: timeout,
	}
}

// Enqueue adds a request to the queue. Blocks until resolved or timeout.
func (q *Queue) Enqueue(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	q.mu.Lock()
	if len(q.entries) >= q.maxSize {
		q.mu.Unlock()
		return nil, ErrQueueFull
	}

	entry := &QueueEntry{
		Request:    req,
		ResultCh:   make(chan QueueResult, 1),
		EnqueuedAt: time.Now(),
	}
	q.entries = append(q.entries, entry)
	q.mu.Unlock()

	// Wait for result or timeout
	timeout := q.timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	select {
	case result := <-entry.ResultCh:
		return result.Session, result.Err
	case <-time.After(timeout):
		q.remove(entry)
		return nil, ErrQueueTimeout
	case <-ctx.Done():
		q.remove(entry)
		return nil, ctx.Err()
	}
}

// TryDequeue returns the oldest queued entry, or nil if empty.
func (q *Queue) TryDequeue() *QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 {
		return nil
	}

	entry := q.entries[0]
	q.entries = q.entries[1:]
	return entry
}

// Depth returns the number of entries in the queue.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// remove removes an entry from the queue (e.g., on timeout).
func (q *Queue) remove(target *QueueEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, entry := range q.entries {
		if entry == target {
			q.entries = append(q.entries[:i], q.entries[i+1:]...)
			return
		}
	}
}
