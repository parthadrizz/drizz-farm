// Package store provides SQLite-based persistence for session history,
// AVD creation records, and usage tracking.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the SQLite persistence layer.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at the given data directory.
func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("store: create dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "drizz-farm.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// WAL mode for concurrent reads
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates tables and runs schema migrations (e.g. adding node_name column).
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			profile TEXT NOT NULL,
			platform TEXT NOT NULL DEFAULT 'android',
			instance_id TEXT NOT NULL,
			device_name TEXT NOT NULL DEFAULT '',
			serial TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			adb_port INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			released_at DATETIME,
			duration_seconds INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS avd_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			avd_name TEXT NOT NULL,
			profile_name TEXT NOT NULL,
			device TEXT NOT NULL,
			system_image TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			instance_id TEXT,
			session_id TEXT,
			detail TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
		CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
		CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

		-- Per-AVD reservations. Keyed by avd_name (stable across daemon
		-- restarts, unlike the runtime instance ID). On start we re-apply
		-- Reserved=true to any instance whose AVD is in this table.
		CREATE TABLE IF NOT EXISTS reservations (
			avd_name TEXT PRIMARY KEY,
			label TEXT NOT NULL DEFAULT '',
			reserved_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	// Migration: add node_name column if missing
	s.db.Exec(`ALTER TABLE sessions ADD COLUMN node_name TEXT NOT NULL DEFAULT ''`)

	// Migration: capabilities JSON blob per session (record_video,
	// capture_logcat, etc.). Errors intentionally ignored — ALTER
	// fails with "duplicate column" when the migration's already been
	// applied, which is the happy path.
	s.db.Exec(`ALTER TABLE sessions ADD COLUMN capabilities TEXT NOT NULL DEFAULT ''`)

	return nil
}

// --- Session History ---

// RecordSession saves a completed session to history.
func (s *Store) RecordSession(id, profile, platform, instanceID, deviceName, serial, host, source, state, nodeName string, adbPort int, createdAt time.Time, releasedAt *time.Time) error {
	duration := 0
	var released *string
	if releasedAt != nil {
		duration = int(releasedAt.Sub(createdAt).Seconds())
		t := releasedAt.Format(time.RFC3339)
		released = &t
	}

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO sessions (id, profile, platform, instance_id, device_name, serial, host, adb_port, source, state, node_name, created_at, released_at, duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, profile, platform, instanceID, deviceName, serial, host, adbPort, source, state, nodeName,
		createdAt.Format(time.RFC3339), released, duration)
	return err
}

// CleanupZombieSessions marks all active/queued sessions as interrupted.
// Called at daemon startup — if the daemon crashed, the emulators died
// but the session records remain stuck in 'active' state.
// Returns the number of sessions cleaned up.
func (s *Store) CleanupZombieSessions() (int, error) {
	res, err := s.db.Exec(`
		UPDATE sessions
		SET state = 'interrupted',
		    released_at = ?,
		    duration_seconds = CAST((julianday(?) - julianday(created_at)) * 86400 AS INTEGER)
		WHERE state IN ('active', 'queued')`,
		time.Now().Format(time.RFC3339),
		time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SessionHistory returns recent sessions.
type SessionRecord struct {
	ID              string  `json:"id"`
	NodeName        string  `json:"node_name"`
	Profile         string  `json:"profile"`
	Platform        string  `json:"platform"`
	DeviceName      string  `json:"device_name"`
	Serial          string  `json:"serial"`
	Source          string  `json:"source"`
	State           string  `json:"state"`
	CreatedAt       string  `json:"created_at"`
	ReleasedAt      *string `json:"released_at"`
	DurationSeconds int     `json:"duration_seconds"`
}

// SessionsSince returns the count of sessions created after the given time.
// Used by the telemetry heartbeat to report rough activity.
func (s *Store) SessionsSince(since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE created_at > ?`, since.Format(time.RFC3339)).Scan(&n)
	return n, err
}

// SessionHistory returns the most recent sessions from SQLite, ordered by creation time.
func (s *Store) SessionHistory(limit int) ([]SessionRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, node_name, profile, platform, device_name, serial, source, state, created_at, released_at, duration_seconds
		FROM sessions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.NodeName, &r.Profile, &r.Platform, &r.DeviceName, &r.Serial, &r.Source, &r.State, &r.CreatedAt, &r.ReleasedAt, &r.DurationSeconds); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}

// --- AVD History ---

// RecordAVDCreation saves an AVD creation event to history.
func (s *Store) RecordAVDCreation(avdName, profileName, device, systemImage string) error {
	_, err := s.db.Exec(`INSERT INTO avd_history (avd_name, profile_name, device, system_image) VALUES (?, ?, ?, ?)`,
		avdName, profileName, device, systemImage)
	return err
}

// --- Events ---

// RecordEvent logs a timestamped event (e.g. session_created, device_booted) to SQLite.
func (s *Store) RecordEvent(eventType, instanceID, sessionID, detail string) error {
	_, err := s.db.Exec(`INSERT INTO events (event_type, instance_id, session_id, detail) VALUES (?, ?, ?, ?)`,
		eventType, instanceID, sessionID, detail)
	return err
}

type EventRecord struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Instance  string `json:"instance_id"`
	Session   string `json:"session_id"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// RecentEvents returns the most recent events from the events log.
func (s *Store) RecentEvents(limit int) ([]EventRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, event_type, COALESCE(instance_id,''), COALESCE(session_id,''), COALESCE(detail,''), created_at
		FROM events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []EventRecord
	for rows.Next() {
		var r EventRecord
		if err := rows.Scan(&r.ID, &r.Type, &r.Instance, &r.Session, &r.Detail, &r.CreatedAt); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}

// --- Reservations ---

// SetReservation marks an AVD as reserved with an optional label.
// Idempotent: repeating with a new label replaces the previous one.
func (s *Store) SetReservation(avdName, label string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO reservations (avd_name, label) VALUES (?, ?)
		ON CONFLICT(avd_name) DO UPDATE SET label = excluded.label, reserved_at = CURRENT_TIMESTAMP
	`, avdName, label)
	return err
}

// ClearReservation removes the reservation for an AVD. No-op if not reserved.
func (s *Store) ClearReservation(avdName string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM reservations WHERE avd_name = ?`, avdName)
	return err
}

// LoadReservations returns all current reservations as a map
// avd_name → label. Used at daemon start to re-apply reservations
// to freshly-discovered instances.
func (s *Store) LoadReservations() (map[string]string, error) {
	out := map[string]string{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT avd_name, label FROM reservations`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, label string
		if err := rows.Scan(&name, &label); err == nil {
			out[name] = label
		}
	}
	return out, nil
}

// EventsForSession returns all events recorded for a given session
// ID, oldest first. Used by the playback timeline to merge server-side
// lifecycle events (session_created, session_released, etc.) with
// parsed logcat + HAR data.
func (s *Store) EventsForSession(sessionID string) ([]EventRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT id, event_type, COALESCE(instance_id,''), COALESCE(session_id,''), COALESCE(detail,''), created_at
		FROM events WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRecord
	for rows.Next() {
		var r EventRecord
		if err := rows.Scan(&r.ID, &r.Type, &r.Instance, &r.Session, &r.Detail, &r.CreatedAt); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
