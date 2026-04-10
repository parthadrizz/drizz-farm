package store

import (
	"os"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	// DB file should exist
	if _, err := os.Stat(dir + "/drizz-farm.db"); err != nil {
		t.Errorf("expected db file, got %v", err)
	}
}

func TestRecordSession(t *testing.T) {
	s := testStore(t)

	now := time.Now()
	released := now.Add(5 * time.Minute)

	err := s.RecordSession("sess-1", "pixel7", "android", "inst-1", "Pixel 7", "emulator-5554", "192.168.1.1", "cli", "released", "test-node", 5555, now, &released)
	if err != nil {
		t.Fatalf("RecordSession error: %v", err)
	}

	// Query back
	records, err := s.SessionHistory(10)
	if err != nil {
		t.Fatalf("SessionHistory error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ID != "sess-1" {
		t.Errorf("expected ID 'sess-1', got '%s'", records[0].ID)
	}
	if records[0].DurationSeconds != 300 {
		t.Errorf("expected 300s duration, got %d", records[0].DurationSeconds)
	}
	if records[0].State != "released" {
		t.Errorf("expected state 'released', got '%s'", records[0].State)
	}
}

func TestSessionHistoryLimit(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 10; i++ {
		now := time.Now()
		s.RecordSession("sess-"+string(rune('a'+i)), "pixel7", "android", "inst-1", "Pixel 7", "emulator-5554", "192.168.1.1", "cli", "released", "test-node", 5555, now, nil)
	}

	records, _ := s.SessionHistory(3)
	if len(records) != 3 {
		t.Errorf("expected 3 records with limit, got %d", len(records))
	}
}

func TestRecordEvent(t *testing.T) {
	s := testStore(t)

	err := s.RecordEvent("session_created", "inst-1", "sess-1", "profile=pixel7")
	if err != nil {
		t.Fatalf("RecordEvent error: %v", err)
	}

	s.RecordEvent("session_released", "inst-1", "sess-1", "duration=300s")
	s.RecordEvent("device_booted", "inst-2", "", "avd=drizz_pixel7_0")

	events, err := s.RecentEvents(10)
	if err != nil {
		t.Fatalf("RecentEvents error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != "device_booted" { // Most recent first
		t.Errorf("expected most recent event first, got '%s'", events[0].Type)
	}
}

func TestRecordAVDCreation(t *testing.T) {
	s := testStore(t)

	err := s.RecordAVDCreation("drizz_pixel7_0", "pixel7", "pixel_7", "system-images;android-34;google_apis;arm64-v8a")
	if err != nil {
		t.Fatalf("RecordAVDCreation error: %v", err)
	}
}

func TestSessionUpsert(t *testing.T) {
	s := testStore(t)

	now := time.Now()
	// Insert
	s.RecordSession("sess-1", "pixel7", "android", "inst-1", "Pixel 7", "emulator-5554", "192.168.1.1", "cli", "active", "test-node", 5555, now, nil)

	// Update (upsert)
	released := now.Add(10 * time.Minute)
	s.RecordSession("sess-1", "pixel7", "android", "inst-1", "Pixel 7", "emulator-5554", "192.168.1.1", "cli", "released", "test-node", 5555, now, &released)

	records, _ := s.SessionHistory(10)
	if len(records) != 1 {
		t.Fatalf("expected 1 record after upsert, got %d", len(records))
	}
	if records[0].State != "released" {
		t.Errorf("expected updated state 'released', got '%s'", records[0].State)
	}
}

func TestEmptyHistory(t *testing.T) {
	s := testStore(t)

	records, err := s.SessionHistory(10)
	if err != nil {
		t.Fatalf("SessionHistory error: %v", err)
	}
	if records != nil && len(records) != 0 {
		t.Errorf("expected empty, got %d records", len(records))
	}
}
