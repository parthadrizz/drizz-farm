package api

// Session playback timeline — merges everything the system knows
// about what happened during one session into a single ordered
// event stream the dashboard can overlay on the recorded video.
//
// Sources:
//
//   1. SQLite `events` table — session_created, session_released,
//      session_timed_out, instance state transitions. These are
//      what the BROKER did.
//
//   2. logcat.txt (if capture_logcat was on) — filtered to E (error)
//      and W (warning) lines only. Info/debug logcat is 30k+ lines
//      per session and would drown the timeline; users who want
//      everything download the raw file from /artifacts.
//
//   3. network.har (if capture_network was on) — one entry per
//      request with URL, status, method, duration.
//
//   4. Screenshots dir — filenames encode capture time, so each
//      on-demand screenshot becomes a marker too.
//
// Every event lands as:
//
//   { ts, relative_s, type, level, detail, url?, size? }
//
// where `ts` is the real clock time (ISO 8601) and `relative_s` is
// seconds since the session's `created_at`, i.e. the offset to seek
// the video to. The UI uses `relative_s` for the timeline scrubber
// and `ts` for human-readable context.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/session"
	"github.com/drizz-dev/drizz-farm/internal/store"
)

type timelineHandlers struct {
	broker  *session.Broker
	store   *store.Store
	dataDir string // for locating artifacts/<sid>/ files
}

// TimelineEvent is the unified shape the UI consumes.
type TimelineEvent struct {
	Time       string  `json:"ts"`         // ISO 8601 UTC
	RelativeS  float64 `json:"relative_s"` // seconds offset from session start (for video seek)
	Type       string  `json:"type"`       // "lifecycle" | "logcat" | "network" | "screenshot"
	Level      string  `json:"level,omitempty"`
	Message    string  `json:"message"`
	URL        string  `json:"url,omitempty"`
	Status     int     `json:"status,omitempty"`
	Method     string  `json:"method,omitempty"`
	DurationMs int     `json:"duration_ms,omitempty"`
}

// Get → GET /api/v1/sessions/{id}/timeline
func (h *timelineHandlers) Get(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "id")

	// Figure out t0 — the session's creation time — so every event
	// gets a consistent relative_s offset. Prefer the live session
	// if still active; fall back to the SQLite sessions table for
	// already-released sessions.
	t0, createdAtStr, ok := h.sessionStart(sid)
	if !ok {
		JSON(w, http.StatusNotFound, ErrorResponse{
			Error: "not_found", Message: "session not found", Code: 404,
		})
		return
	}

	var events []TimelineEvent

	// 1. SQLite lifecycle events
	if h.store != nil {
		recs, _ := h.store.EventsForSession(sid)
		for _, e := range recs {
			ts, err := parseStoreTime(e.CreatedAt)
			if err != nil {
				continue
			}
			events = append(events, TimelineEvent{
				Time:      ts.UTC().Format(time.RFC3339Nano),
				RelativeS: ts.Sub(t0).Seconds(),
				Type:      "lifecycle",
				Message:   e.Type,
				Level:     levelFromLifecycle(e.Type),
			})
		}
	}

	// 2. logcat.txt (E/W only)
	artifactsDir := filepath.Join(h.dataDir, "artifacts", sid)
	if logcatEvents, err := parseLogcat(filepath.Join(artifactsDir, "logcat.txt"), t0); err == nil {
		events = append(events, logcatEvents...)
	}

	// 3. network.har
	if harEvents, err := parseHAR(filepath.Join(artifactsDir, "network.har"), t0); err == nil {
		events = append(events, harEvents...)
	}

	// 4. screenshots by filename timestamp
	if sEvents, err := parseScreenshotDir(artifactsDir, t0); err == nil {
		events = append(events, sEvents...)
	}

	// Drop events that happened before the session was even created.
	// Adb logcat dumps the pre-session ring buffer on start, so the
	// first few hundred lines are from the emulator's boot history,
	// not from this run — they used to show up at relative_s = -15s
	// on the player's timeline and confuse the user about when "0:00"
	// actually was.
	filtered := events[:0]
	for _, e := range events {
		if e.RelativeS >= 0 {
			filtered = append(filtered, e)
		}
	}
	events = filtered

	// Sort by relative_s — merges all four sources cleanly.
	sort.Slice(events, func(i, j int) bool {
		return events[i].RelativeS < events[j].RelativeS
	})

	// Response includes the artifact URL for convenience so the UI
	// doesn't have to make a second call for the <video src=...>.
	JSON(w, http.StatusOK, map[string]any{
		"session_id":   sid,
		"started_at":   createdAtStr,
		"video_url":    fmt.Sprintf("/api/v1/sessions/%s/artifacts/video.mp4", sid),
		"artifact_dir": fmt.Sprintf("/api/v1/sessions/%s/artifacts", sid),
		"events":       events,
		"total":        len(events),
	})
}

// ---- Session start time resolution ---------------------------------

func (h *timelineHandlers) sessionStart(sid string) (time.Time, string, bool) {
	// Live session first — cheapest path, authoritative for in-flight
	// playback (user watching a session in progress).
	if sess, err := h.broker.Get(sid); err == nil {
		return sess.CreatedAt, sess.CreatedAt.UTC().Format(time.RFC3339Nano), true
	}
	// Released sessions live in SQLite.
	if h.store != nil {
		if recs, err := h.store.SessionHistory(200); err == nil {
			for _, r := range recs {
				if r.ID != sid {
					continue
				}
				if ts, err := parseStoreTime(r.CreatedAt); err == nil {
					return ts, ts.UTC().Format(time.RFC3339Nano), true
				}
			}
		}
	}
	return time.Time{}, "", false
}

// ---- Logcat parser -------------------------------------------------

// logcat threadtime format:
//
//   MM-DD HH:MM:SS.sss  PID  TID LEVEL TAG: MESSAGE
//
// Year is missing (!) from every line, so we infer the year from
// session start — a single calendar transition mid-session is a
// ~7-hour edge case we accept getting wrong for now.
// parseLogcat handles `adb logcat -v epoch` output. Each line starts
// with a Unix-timestamp prefix (seconds.millis), which dodges the TZ
// drift between emulator and host that the old threadtime parser used
// to silently miss. Falls back to the legacy "MM-DD HH:MM:SS.mmm"
// format for captures taken with older daemons so old session
// artifacts still render on the timeline.
func parseLogcat(path string, t0 time.Time) ([]TimelineEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	var out []TimelineEvent
	year := t0.Year()

	for scanner.Scan() {
		raw := scanner.Text()
		// adb logcat right-pads the epoch timestamp with leading spaces
		// for column alignment (e.g. "         1729795316.197 …").
		// Trim them so the first field parses cleanly.
		line := strings.TrimLeft(raw, " ")
		if len(line) < 15 {
			continue
		}

		var ts time.Time
		var restStart int
		// Epoch format: "1729795316.197 <tid> <tid> <lvl> tag: msg"
		if parts := strings.SplitN(line, " ", 2); len(parts) == 2 && strings.Contains(parts[0], ".") {
			if secStr, msStr, ok := strings.Cut(parts[0], "."); ok {
				if sec, err1 := strconv.ParseInt(secStr, 10, 64); err1 == nil {
					if ms, err2 := strconv.ParseInt(msStr, 10, 64); err2 == nil && sec > 1_000_000_000 {
						ts = time.Unix(sec, ms*int64(time.Millisecond))
						restStart = len(parts[0]) + 1
					}
				}
			}
		}
		// Legacy threadtime fallback: "04-24 23:38:10.197 ..."
		if ts.IsZero() && len(line) >= 18 {
			if parsed, err := time.ParseInLocation("2006 01-02 15:04:05.000",
				fmt.Sprintf("%d %s", year, line[:18]), t0.Location()); err == nil {
				ts = parsed
				restStart = 18
			}
		}
		if ts.IsZero() || restStart >= len(line) {
			continue
		}
		rest := line[restStart:]

		level := ""
		if strings.Contains(rest, " E ") {
			level = "error"
		} else if strings.Contains(rest, " W ") {
			level = "warn"
		} else {
			continue // skip info/debug/verbose
		}
		msg := rest
		if i := strings.Index(rest, ": "); i > 0 {
			msg = strings.TrimSpace(rest[i+2:])
		}
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		out = append(out, TimelineEvent{
			Time:      ts.UTC().Format(time.RFC3339Nano),
			RelativeS: ts.Sub(t0).Seconds(),
			Type:      "logcat",
			Level:     level,
			Message:   msg,
		})
	}
	return out, scanner.Err()
}

// ---- HAR parser ----------------------------------------------------

type harFile struct {
	Log struct {
		Entries []struct {
			StartedDateTime string  `json:"startedDateTime"`
			Time            float64 `json:"time"`
			Request         struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
			Response struct {
				Status int `json:"status"`
			} `json:"response"`
		} `json:"entries"`
	} `json:"log"`
}

func parseHAR(path string, t0 time.Time) ([]TimelineEvent, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var har harFile
	if err := json.Unmarshal(b, &har); err != nil {
		return nil, err
	}
	out := make([]TimelineEvent, 0, len(har.Log.Entries))
	for _, e := range har.Log.Entries {
		ts, err := time.Parse(time.RFC3339Nano, e.StartedDateTime)
		if err != nil {
			if ts, err = time.Parse(time.RFC3339, e.StartedDateTime); err != nil {
				continue
			}
		}
		level := "info"
		if e.Response.Status >= 400 {
			level = "error"
		} else if e.Response.Status >= 300 {
			level = "warn"
		}
		out = append(out, TimelineEvent{
			Time:       ts.UTC().Format(time.RFC3339Nano),
			RelativeS:  ts.Sub(t0).Seconds(),
			Type:       "network",
			Level:      level,
			Message:    fmt.Sprintf("%s %s", e.Request.Method, truncateURL(e.Request.URL, 80)),
			URL:        e.Request.URL,
			Status:     e.Response.Status,
			Method:     e.Request.Method,
			DurationMs: int(e.Time),
		})
	}
	return out, nil
}

// ---- Screenshot dir parser ----------------------------------------

// Filenames we emit:
//   screenshot_20060102_150405.png
//   (from handlers_recording.go::Screenshot)
func parseScreenshotDir(artifactsDir string, t0 time.Time) ([]TimelineEvent, error) {
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		return nil, err
	}
	var out []TimelineEvent
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "screenshot_") || filepath.Ext(name) != ".png" {
			continue
		}
		// Parse the stamp from the filename.
		stamp := strings.TrimSuffix(strings.TrimPrefix(name, "screenshot_"), ".png")
		ts, err := time.ParseInLocation("20060102_150405", stamp, t0.Location())
		if err != nil {
			// Fall back to file mtime.
			if info, ierr := e.Info(); ierr == nil {
				ts = info.ModTime()
			} else {
				continue
			}
		}
		out = append(out, TimelineEvent{
			Time:      ts.UTC().Format(time.RFC3339Nano),
			RelativeS: ts.Sub(t0).Seconds(),
			Type:      "screenshot",
			Message:   name,
			URL:       name, // UI builds /artifacts URL from session id + filename
		})
	}
	return out, nil
}

// ---- helpers ------------------------------------------------------

func parseStoreTime(s string) (time.Time, error) {
	// SQLite stores timestamps in a variety of shapes depending on how
	// they landed. Try RFC3339 first (most of our writes), then the
	// plain SQL DATETIME format, then local-time fallback.
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	// SQLite's default CURRENT_TIMESTAMP is UTC "YYYY-MM-DD HH:MM:SS".
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", s)
}

func levelFromLifecycle(t string) string {
	switch t {
	case "session_timed_out", "session_error", "instance_error":
		return "warn"
	default:
		return "info"
	}
}

func truncateURL(u string, max int) string {
	if len(u) <= max {
		return u
	}
	return u[:max] + "…"
}

// Stub retained for forwards-compat if we add duration-parse branches
// for funky logcat timestamp variants later.
var _ = strconv.Itoa
