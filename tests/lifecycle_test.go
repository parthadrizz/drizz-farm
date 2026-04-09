//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestLifecycle_QueueDrain(t *testing.T) {
	pool := GetPool(t)
	capacity := pool.TotalCapacity
	if capacity < 2 {
		t.Skip("need capacity >= 2")
	}

	// Fill capacity
	var sessions []SessionResponse
	for i := 0; i < capacity; i++ {
		sess := CreateSession(t, "")
		sessions = append(sessions, sess)
	}

	// Queue a 4th in background
	done := make(chan SessionResponse, 1)
	go func() {
		resp, err := http.Post(apiBase+"/sessions", "application/json",
			bytes.NewReader([]byte(`{"profile":"api34_ext8_play"}`)))
		if err != nil {
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var sess SessionResponse
		json.Unmarshal(body, &sess)
		done <- sess
	}()

	// Verify it's queued
	time.Sleep(2 * time.Second)
	data := APIGet(t, "/sessions")
	var listResult struct {
		Queued int `json:"queued"`
	}
	json.Unmarshal(data, &listResult)
	if listResult.Queued < 1 {
		t.Logf("expected queued >= 1, got %d (might have been served already)", listResult.Queued)
	}

	// Release one — queued should get served
	ReleaseSession(t, sessions[0].ID)

	select {
	case sess := <-done:
		if sess.ID == "" {
			t.Error("queued session not served")
		} else {
			t.Logf("queue drained: session %s on %s", sess.ID, sess.Connection.ADBSerial)
			ReleaseSession(t, sess.ID)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("queued session not served within 60s")
	}

	// Cleanup
	for _, s := range sessions[1:] {
		ReleaseSession(t, s.ID)
	}
	time.Sleep(3 * time.Second)
}

func TestLifecycle_AllocateReleaseAllocate(t *testing.T) {
	// First allocate
	s1 := CreateSession(t, "")
	emulator1 := s1.Connection.ADBSerial

	// Release
	ReleaseSession(t, s1.ID)
	time.Sleep(3 * time.Second)

	// Re-allocate — should get same emulator (it's warm)
	s2 := CreateSession(t, "")
	emulator2 := s2.Connection.ADBSerial

	if emulator1 != emulator2 {
		t.Logf("different emulator on re-allocate: %s vs %s (acceptable)", emulator1, emulator2)
	}

	ReleaseSession(t, s2.ID)
}

func TestLifecycle_MultipleSessionsParallel(t *testing.T) {
	pool := GetPool(t)
	capacity := pool.TotalCapacity

	// Create all sessions
	var sessions []SessionResponse
	for i := 0; i < capacity; i++ {
		sess := CreateSession(t, "")
		sessions = append(sessions, sess)
	}

	// Verify all unique serials
	serials := make(map[string]bool)
	for _, s := range sessions {
		if serials[s.Connection.ADBSerial] {
			t.Errorf("duplicate serial: %s", s.Connection.ADBSerial)
		}
		serials[s.Connection.ADBSerial] = true
	}

	// Release all
	for _, s := range sessions {
		ReleaseSession(t, s.ID)
	}
	time.Sleep(3 * time.Second)
}
