// Package capture centralizes per-session artifact recording —
// screen video, logcat, and (future) network HAR — so the session
// broker can declaratively start/stop capture based on session
// capabilities without duplicating logic with the HTTP handlers.
//
// Design:
//   - One Service per daemon, owns the data directory + adb binary
//     path + a lock-protected map of active captures.
//   - StartVideo / StartLogcat kick off the underlying process and
//     return once it's running; output accumulates in a file under
//     $DATA_DIR/artifacts/$SESSION_ID/.
//   - StopAll drains every capture for a session and waits for each
//     to finalize its file before returning.
//   - Idempotent: starting twice on the same session returns the
//     same active capture; stopping a non-existent one is a no-op.
package capture

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Service manages active captures per session. Thread-safe.
type Service struct {
	mu sync.Mutex

	adbPath string
	dataDir string

	videos  map[string]*videoCapture  // keyed by sessionID
	logcats map[string]*logcatCapture // keyed by sessionID
}

type videoCapture struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	devicePath string
	localPath  string
	serial     string
	startedAt  time.Time
}

type logcatCapture struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	file      *os.File
	localPath string
	startedAt time.Time
}

// NewService wires up the capture service with the ADB binary path
// and the daemon's data directory. Artifacts land at
// <dataDir>/artifacts/<session_id>/{video.mp4, logcat.txt, ...}.
func NewService(adbPath, dataDir string) *Service {
	return &Service{
		adbPath: adbPath,
		dataDir: dataDir,
		videos:  map[string]*videoCapture{},
		logcats: map[string]*logcatCapture{},
	}
}

// ArtifactsDir returns the directory where a session's artifacts live.
// Created lazily.
func (s *Service) ArtifactsDir(sessionID string) string {
	d := filepath.Join(s.dataDir, "artifacts", sessionID)
	_ = os.MkdirAll(d, 0755)
	return d
}

// StartVideo begins a screenrecord on the device, writing in 180s
// chunks (Android's screenrecord hard cap) and pulling each chunk
// back to host storage. For v0 we just capture a single 180s segment;
// if the session outlives the segment, we don't yet restart — TODO.
func (s *Service) StartVideo(sessionID, serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.videos[sessionID]; ok {
		return nil // idempotent
	}
	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir artifacts: %w", err)
	}
	devicePath := "/sdcard/drizz_session_" + sessionID + ".mp4"
	localPath := filepath.Join(dir, "video.mp4")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.adbPath, "-s", serial, "shell",
		"screenrecord", "--time-limit", "180", devicePath)
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start screenrecord: %w", err)
	}
	s.videos[sessionID] = &videoCapture{
		cmd:        cmd,
		cancel:     cancel,
		devicePath: devicePath,
		localPath:  localPath,
		serial:     serial,
		startedAt:  time.Now(),
	}
	log.Info().Str("session", sessionID).Str("serial", serial).Msg("capture: video started")
	return nil
}

// StopVideo sends SIGINT to screenrecord so the MP4 finalizes cleanly,
// waits for the process, then pulls the file off the device. Errors
// are logged and swallowed — stop-on-release shouldn't fail the
// release. Returns the final artifact path (empty if nothing was
// captured or the pull failed).
func (s *Service) StopVideo(sessionID string) string {
	s.mu.Lock()
	cap, ok := s.videos[sessionID]
	if ok {
		delete(s.videos, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ""
	}

	// SIGINT makes screenrecord finalize its MP4 header. If it's
	// already dead or blocked, cancel the ctx as a fallback.
	if cap.cmd.Process != nil {
		_ = cap.cmd.Process.Signal(os.Interrupt)
	}
	done := make(chan struct{})
	go func() { cap.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cap.cancel()
		<-done
	}

	// Pull the finalized MP4 off the device.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pullCmd := exec.CommandContext(ctx, s.adbPath, "-s", cap.serial, "pull", cap.devicePath, cap.localPath)
	if out, err := pullCmd.CombinedOutput(); err != nil {
		log.Warn().Err(err).Str("session", sessionID).Str("out", string(out)).Msg("capture: adb pull video failed")
		return ""
	}
	// Best-effort cleanup of the on-device file.
	rmCtx, rmCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rmCancel()
	_ = exec.CommandContext(rmCtx, s.adbPath, "-s", cap.serial, "shell", "rm", "-f", cap.devicePath).Run()

	log.Info().Str("session", sessionID).Str("path", cap.localPath).Msg("capture: video saved")
	return cap.localPath
}

// StartLogcat tails `adb logcat -b all` for the session, writing
// everything to logcat.txt under the artifacts dir. Idempotent.
func (s *Service) StartLogcat(sessionID, serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.logcats[sessionID]; ok {
		return nil
	}
	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir artifacts: %w", err)
	}
	localPath := filepath.Join(dir, "logcat.txt")
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create logcat file: %w", err)
	}

	// Clear device buffer first so we capture events from session
	// start onward, not the stale history sitting on the emulator.
	clearCtx, clearCancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = exec.CommandContext(clearCtx, s.adbPath, "-s", serial, "logcat", "-c").Run()
	clearCancel()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.adbPath, "-s", serial, "logcat", "-b", "all", "-v", "threadtime")
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		f.Close()
		cancel()
		return fmt.Errorf("start logcat: %w", err)
	}
	s.logcats[sessionID] = &logcatCapture{
		cmd:       cmd,
		cancel:    cancel,
		file:      f,
		localPath: localPath,
		startedAt: time.Now(),
	}
	log.Info().Str("session", sessionID).Str("serial", serial).Msg("capture: logcat started")
	return nil
}

// StopLogcat cancels the tail, flushes the file, and returns the
// finalized path. Errors logged + swallowed.
func (s *Service) StopLogcat(sessionID string) string {
	s.mu.Lock()
	cap, ok := s.logcats[sessionID]
	if ok {
		delete(s.logcats, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ""
	}
	cap.cancel()
	done := make(chan struct{})
	go func() { cap.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	_ = cap.file.Sync()
	_ = cap.file.Close()
	log.Info().Str("session", sessionID).Str("path", cap.localPath).Msg("capture: logcat saved")
	return cap.localPath
}

// StopAll stops every active capture for a session. Safe to call
// when nothing's active.
func (s *Service) StopAll(sessionID string) {
	s.StopVideo(sessionID)
	s.StopLogcat(sessionID)
}

// ArtifactFile is a single persisted artifact for a session.
type ArtifactFile struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

// List returns every file in the session's artifacts dir with its
// type inferred from the filename (video.mp4 → video, logcat.txt →
// logcat, anything else → "other"). URL is the relative path users
// can append to the daemon base URL to download.
func (s *Service) List(sessionID string) []ArtifactFile {
	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ArtifactFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		t := inferType(e.Name())
		out = append(out, ArtifactFile{
			Type:     t,
			Filename: e.Name(),
			Size:     info.Size(),
			URL:      fmt.Sprintf("/api/v1/sessions/%s/artifacts/%s", sessionID, e.Name()),
		})
	}
	return out
}

// ArtifactPath returns the on-disk path for a named artifact file.
// Empty when the session has no artifacts dir or the name doesn't
// exist (so the handler can 404 cleanly).
func (s *Service) ArtifactPath(sessionID, filename string) string {
	// Refuse path escapes.
	if filepath.Base(filename) != filename {
		return ""
	}
	p := filepath.Join(s.dataDir, "artifacts", sessionID, filename)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func inferType(filename string) string {
	switch {
	case filename == "video.mp4":
		return "video"
	case filename == "logcat.txt":
		return "logcat"
	case filename == "network.har" || filename == "network.pcap":
		return "network"
	case filepath.Ext(filename) == ".png":
		return "screenshot"
	default:
		return "other"
	}
}
