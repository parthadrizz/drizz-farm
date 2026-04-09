package session

import (
	"testing"
	"time"
)

func TestSessionStates(t *testing.T) {
	sess := &Session{
		ID:        "test-1",
		State:     SessionActive,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	if !sess.IsActive() {
		t.Error("expected active session to be active")
	}
	if sess.IsExpired() {
		t.Error("expected session to not be expired")
	}

	sess.State = SessionReleased
	if sess.IsActive() {
		t.Error("expected released session to not be active")
	}
}

func TestSessionExpiry(t *testing.T) {
	sess := &Session{
		ID:        "test-1",
		State:     SessionActive,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}

	if !sess.IsExpired() {
		t.Error("expected past-expiry session to be expired")
	}
}

func TestSessionStateString(t *testing.T) {
	if SessionActive.String() != "active" {
		t.Errorf("expected 'active', got '%s'", SessionActive.String())
	}
	if SessionQueued.String() != "queued" {
		t.Errorf("expected 'queued', got '%s'", SessionQueued.String())
	}
	if SessionTimedOut.String() != "timed_out" {
		t.Errorf("expected 'timed_out', got '%s'", SessionTimedOut.String())
	}
}

func TestQueueDepth(t *testing.T) {
	q := NewQueue(10, 5*time.Minute)
	if q.Depth() != 0 {
		t.Errorf("expected empty queue, got depth %d", q.Depth())
	}
}

func TestQueueFull(t *testing.T) {
	q := NewQueue(1, 5*time.Minute)

	// Fill the queue by adding an entry directly
	q.mu.Lock()
	q.entries = append(q.entries, &QueueEntry{
		Request:    CreateSessionRequest{Profile: "test"},
		ResultCh:   make(chan QueueResult, 1),
		EnqueuedAt: time.Now(),
	})
	q.mu.Unlock()

	// Now enqueue should fail
	ctx := t.Context()
	_, err := q.Enqueue(ctx, CreateSessionRequest{Profile: "test"})
	if err != ErrQueueFull {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestQueueTryDequeueEmpty(t *testing.T) {
	q := NewQueue(10, 5*time.Minute)
	entry := q.TryDequeue()
	if entry != nil {
		t.Error("expected nil from empty queue")
	}
}

func TestDetectLANIP(t *testing.T) {
	ip := detectLANIP()
	if ip == "" {
		t.Error("expected non-empty IP")
	}
	// Should either be a real LAN IP or fallback to localhost
	t.Logf("detected LAN IP: %s", ip)
}
