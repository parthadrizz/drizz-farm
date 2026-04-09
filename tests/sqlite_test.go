//go:build integration

package tests

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSQLite_SessionPersistence verifies that creating and releasing a session
// actually writes records to SQLite, and the history API returns them.
func TestSQLite_SessionPersistence(t *testing.T) {
	// Create a session
	sess := CreateSession(t, "")
	if sess.ID == "" {
		t.Fatal("session not created")
	}

	// Check history — should have the active session
	time.Sleep(500 * time.Millisecond)
	data := APIGet(t, "/history/sessions?limit=10")
	var hist struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(data, &hist); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}

	found := false
	for _, s := range hist.Sessions {
		if s["id"] == sess.ID {
			found = true
			if s["state"] != "active" {
				t.Errorf("expected state 'active', got '%s'", s["state"])
			}
			if s["profile"] != sess.Profile {
				t.Errorf("expected profile '%s', got '%s'", sess.Profile, s["profile"])
			}
			break
		}
	}
	if !found {
		t.Errorf("session %s not found in history (got %d records)", sess.ID, len(hist.Sessions))
	}

	// Release the session
	ReleaseSession(t, sess.ID)
	time.Sleep(1 * time.Second)

	// Check history — should now show released
	data = APIGet(t, "/history/sessions?limit=10")
	json.Unmarshal(data, &hist)

	found = false
	for _, s := range hist.Sessions {
		if s["id"] == sess.ID {
			found = true
			if s["state"] != "released" {
				t.Errorf("expected state 'released' after release, got '%s'", s["state"])
			}
			dur, _ := s["duration_seconds"].(float64)
			t.Logf("session duration: %fs", dur)
			// Duration can be 0 if released within same second — that's ok
			break
		}
	}
	if !found {
		t.Errorf("released session %s not found in history", sess.ID)
	}
}

// TestSQLite_EventsPersistence verifies events are recorded.
func TestSQLite_EventsPersistence(t *testing.T) {
	// Create and release a session to generate events
	sess := CreateSession(t, "")
	ReleaseSession(t, sess.ID)
	time.Sleep(1 * time.Second)

	data := APIGet(t, "/history/events?limit=20")
	var events struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}

	if len(events.Events) < 2 {
		t.Errorf("expected at least 2 events (create + release), got %d", len(events.Events))
	}

	// Check event types
	types := make(map[string]bool)
	for _, e := range events.Events {
		if typ, ok := e["type"].(string); ok {
			types[typ] = true
		}
	}

	if !types["session_created"] {
		t.Error("expected 'session_created' event")
	}
	if !types["session_released"] {
		t.Error("expected 'session_released' event")
	}
}

// TestSQLite_HistoryLimitWorks verifies the limit parameter.
func TestSQLite_HistoryLimitWorks(t *testing.T) {
	data := APIGet(t, "/history/sessions?limit=1")
	var hist struct {
		Sessions []map[string]any `json:"sessions"`
	}
	json.Unmarshal(data, &hist)

	if len(hist.Sessions) > 1 {
		t.Errorf("expected at most 1 session with limit=1, got %d", len(hist.Sessions))
	}
}
