package capture

// Retention sweeper — periodically deletes artifact directories
// older than the daemon's retention window. The window is read from
// config (default 7 days). Individual sessions can override via
// Capabilities.RetentionHours at session creation time; the broker
// writes that per-session override into a sidecar `.retention` file
// inside the artifacts dir so this sweeper doesn't need to crack
// open the SQLite sessions table.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// StartRetentionSweeper runs a periodic scan of the artifacts
// directory and removes per-session subdirs whose most-recent file
// is older than the retention window for that session.
//
// Cadence is hard-coded at 30 minutes — retention is about disk
// pressure, not second-by-second timing; a coarse sweep is fine and
// cheap. Runs until ctx is cancelled.
func (s *Service) StartRetentionSweeper(ctx context.Context, defaultHours int) {
	if defaultHours <= 0 {
		defaultHours = 24 * 7 // one week default
	}
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		// Fire once at startup so a restart actually reclaims space
		// that piled up while the daemon was off.
		s.sweep(defaultHours)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(defaultHours)
			}
		}
	}()
}

// SetRetentionOverride writes a per-session override file that the
// sweeper honors instead of the default. Called by the broker when
// Capabilities.RetentionHours > 0. Writes `3600` (seconds)-style
// integers rounded to hours.
func (s *Service) SetRetentionOverride(sessionID string, hours int) {
	if hours <= 0 {
		return
	}
	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, ".retention"), []byte(fmt.Sprintf("%d\n", hours)), 0644)
}

// sweep scans the artifacts dir and deletes per-session subdirs
// whose newest file is older than the retention cutoff. -1 in the
// override file means "retain forever" and skips deletion.
func (s *Service) sweep(defaultHours int) {
	root := filepath.Join(s.dataDir, "artifacts")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	now := time.Now()
	deleted := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessDir := filepath.Join(root, e.Name())
		hours := defaultHours
		if b, err := os.ReadFile(filepath.Join(sessDir, ".retention")); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				hours = n
			}
		}
		if hours < 0 {
			continue // retain forever
		}
		cutoff := now.Add(-time.Duration(hours) * time.Hour)

		// Honor skip if any file inside is newer than the cutoff.
		newest := mostRecentMtime(sessDir)
		if newest.IsZero() {
			continue
		}
		if newest.After(cutoff) {
			continue
		}

		if err := os.RemoveAll(sessDir); err != nil {
			log.Warn().Err(err).Str("dir", sessDir).Msg("retention: delete failed")
			continue
		}
		deleted++
		log.Info().Str("session", e.Name()).Dur("age", now.Sub(newest)).Msg("retention: deleted expired artifact dir")
	}
	if deleted > 0 {
		log.Info().Int("deleted", deleted).Msg("retention: sweep complete")
	}
}

// mostRecentMtime returns the newest ModTime of any regular file
// in dir. Empty dirs return zero time (caller skips).
func mostRecentMtime(dir string) time.Time {
	var newest time.Time
	entries, err := os.ReadDir(dir)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
