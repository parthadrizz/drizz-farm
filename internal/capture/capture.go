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

	videos   map[string]*videoCapture   // keyed by sessionID
	logcats  map[string]*logcatCapture  // keyed by sessionID
	networks map[string]*networkCapture // keyed by sessionID

	nextProxyPort int        // rotates 8889..8999 to avoid collisions
	portMu        sync.Mutex // guards nextProxyPort
}

type videoCapture struct {
	cancel     context.CancelFunc
	done       chan struct{} // closed when the chunk loop exits
	serial     string
	sessionID  string
	localPath  string // final merged path (set on stop)
	chunks     []string // ordered list of chunk file paths
	startedAt  time.Time
	mu         sync.Mutex // guards chunks slice
}

type logcatCapture struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	file      *os.File
	localPath string
	startedAt time.Time
}

type networkCapture struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	serial    string
	port      int
	harPath   string
	startedAt time.Time
}

// NewService wires up the capture service with the ADB binary path
// and the daemon's data directory. Artifacts land at
// <dataDir>/artifacts/<session_id>/{video.mp4, logcat.txt, ...}.
func NewService(adbPath, dataDir string) *Service {
	return &Service{
		adbPath:       adbPath,
		dataDir:       dataDir,
		videos:        map[string]*videoCapture{},
		logcats:       map[string]*logcatCapture{},
		networks:      map[string]*networkCapture{},
		nextProxyPort: 8889,
	}
}

// logEvent appends a line to the session's capture.log so a user
// looking at "why is my video missing" can open one file and see what
// happened without needing to tail the daemon stdout. Cheap, crash-safe,
// appears automatically in the artifacts listing.
func (s *Service) logEvent(sessionID, format string, args ...any) {
	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "capture.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

// ArtifactsDir returns the directory where a session's artifacts live.
// Created lazily.
func (s *Service) ArtifactsDir(sessionID string) string {
	d := filepath.Join(s.dataDir, "artifacts", sessionID)
	_ = os.MkdirAll(d, 0755)
	return d
}

// StartVideo begins a long-form screen recording. Android's built-in
// screenrecord has a hard 180-second cap per invocation, so we run a
// loop that launches fresh 180s chunks back-to-back and pulls each
// one off the device as soon as it finalizes. StopVideo cancels the
// loop, pulls any in-flight chunk, then concatenates everything into
// a single video.mp4 using `ffmpeg -f concat -c copy` when ffmpeg is
// available. If ffmpeg isn't installed we leave the chunks in place
// as video_000.mp4, video_001.mp4, … — the unified artifacts
// endpoint surfaces all of them.
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

	ctx, cancel := context.WithCancel(context.Background())
	vc := &videoCapture{
		cancel:    cancel,
		done:      make(chan struct{}),
		serial:    serial,
		sessionID: sessionID,
		startedAt: time.Now(),
	}
	s.videos[sessionID] = vc

	go s.videoChunkLoop(ctx, vc, dir)
	log.Info().Str("session", sessionID).Str("serial", serial).Msg("capture: video started (chunked)")
	s.logEvent(sessionID, "video.start serial=%s", serial)
	return nil
}

// videoChunkLoop records sequential 180s screenrecord chunks until
// the context is cancelled. Each chunk is pulled off the device and
// appended to vc.chunks as soon as it finishes. Runs in its own
// goroutine; closes vc.done on exit so StopVideo can wait cleanly.
func (s *Service) videoChunkLoop(ctx context.Context, vc *videoCapture, dir string) {
	defer close(vc.done)
	idx := 0
	// When chunks fail rapidly in sequence, the emulator is probably
	// gone — the previous version of this loop spun forever in that
	// state, spamming 100 KB+ of capture.log with pull failures. Any
	// chunk that both starts AND ends inside this threshold is a sign
	// adb is returning immediately; two in a row and we bail.
	const rapidFailThreshold = 2 * time.Second
	rapidFailures := 0
	for {
		if ctx.Err() != nil {
			return
		}
		devicePath := fmt.Sprintf("/sdcard/drizz_%s_%03d.mp4", vc.sessionID, idx)
		localPath := filepath.Join(dir, fmt.Sprintf("video_%03d.mp4", idx))
		chunkStart := time.Now()

		// Each screenrecord invocation runs up to 180s. We pass
		// the chunk context (cancelled with the loop) but also rely
		// on screenrecord's own --time-limit for the happy path.
		cmd := exec.CommandContext(ctx, s.adbPath, "-s", vc.serial, "shell",
			"screenrecord", "--time-limit", "180", devicePath)
		if err := cmd.Start(); err != nil {
			log.Warn().Err(err).Str("session", vc.sessionID).Int("chunk", idx).Msg("capture: chunk start failed")
			s.logEvent(vc.sessionID, "video.chunk_start_failed idx=%d err=%v", idx, err)
			return
		}
		s.logEvent(vc.sessionID, "video.chunk_started idx=%d device_path=%s", idx, devicePath)

		// Wait for the chunk to finish OR the loop to be cancelled.
		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()
		select {
		case <-ctx.Done():
			// Stop called. Finalizing the MP4 needs the screenrecord
			// process INSIDE the emulator to receive a clean SIGINT
			// so it writes the moov atom at EOF — otherwise the pulled
			// file has no moov and browsers refuse to play it.
			//
			// Previously we called cmd.Process.Signal(os.Interrupt),
			// but that SIGINTs the host-side `adb` binary, not the
			// emulator-side screenrecord. Adb drops the connection,
			// screenrecord is killed uncleanly by the broken pipe,
			// and the resulting file is unplayable.
			//
			// The right move: use adb to pkill -SIGINT screenrecord
			// on the device directly. Then wait up to 8s for the MP4
			// to be fully flushed before we kill the host adb command.
			sigCtx, sigCancel := context.WithTimeout(context.Background(), 3*time.Second)
			// -2 = SIGINT (numeric form is most portable across
			// Android's toybox pkill variations).
			_ = exec.CommandContext(sigCtx, s.adbPath, "-s", vc.serial, "shell",
				"pkill", "-2", "screenrecord").Run()
			sigCancel()
			select {
			case <-waitCh:
			case <-time.After(8 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-waitCh
			}
		case err := <-waitCh:
			if err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Str("session", vc.sessionID).Int("chunk", idx).Msg("capture: chunk wait returned error (continuing)")
			}
		}

		// Pull the chunk off the device and record it. We do this
		// even when cancelled — partial chunks are better than none.
		pullCtx, pullCancel := context.WithTimeout(context.Background(), 15*time.Second)
		pullCmd := exec.CommandContext(pullCtx, s.adbPath, "-s", vc.serial, "pull", devicePath, localPath)
		pullErr := pullCmd.Run()
		pullCancel()

		// Best-effort cleanup of on-device chunk.
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = exec.CommandContext(rmCtx, s.adbPath, "-s", vc.serial, "shell", "rm", "-f", devicePath).Run()
		rmCancel()

		if pullErr == nil {
			fi, _ := os.Stat(localPath)
			var size int64
			if fi != nil {
				size = fi.Size()
			}
			vc.mu.Lock()
			vc.chunks = append(vc.chunks, localPath)
			vc.mu.Unlock()
			s.logEvent(vc.sessionID, "video.chunk_pulled idx=%d size=%d", idx, size)
			rapidFailures = 0
		} else if fi, err := os.Stat(localPath); err == nil && fi.Size() > 0 {
			// Pull reported an error but we got something locally.
			// Keep it — partial is useful.
			vc.mu.Lock()
			vc.chunks = append(vc.chunks, localPath)
			vc.mu.Unlock()
			s.logEvent(vc.sessionID, "video.chunk_partial idx=%d size=%d pull_err=%v", idx, fi.Size(), pullErr)
			rapidFailures = 0
		} else {
			s.logEvent(vc.sessionID, "video.chunk_pull_failed idx=%d err=%v", idx, pullErr)
			// Fast-fail circuit breaker: if the whole chunk cycle
			// (screenrecord + pull attempt) completed in under 2s,
			// adb is returning immediately — the emulator is gone.
			// Two in a row = bail and let StopVideo/eviction finalize.
			if time.Since(chunkStart) < rapidFailThreshold {
				rapidFailures++
				if rapidFailures >= 2 {
					log.Warn().Str("session", vc.sessionID).Int("idx", idx).Msg("capture: chunk loop exiting — emulator appears gone")
					s.logEvent(vc.sessionID, "video.loop_abort idx=%d reason=rapid_failures", idx)
					return
				}
			} else {
				rapidFailures = 0
			}
		}

		if ctx.Err() != nil {
			return
		}
		idx++
	}
}

// StopVideo cancels the chunk loop, waits for the in-flight chunk
// to be pulled, then either concatenates chunks with ffmpeg into a
// single video.mp4 (ideal) or leaves them as video_000.mp4,
// video_001.mp4, … (fallback). Returns the final path if a merge
// succeeded, else an empty string.
func (s *Service) StopVideo(sessionID string) string {
	s.mu.Lock()
	vc, ok := s.videos[sessionID]
	if ok {
		delete(s.videos, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ""
	}

	vc.cancel()
	<-vc.done // wait for loop to finish pulling its last chunk

	vc.mu.Lock()
	chunks := append([]string(nil), vc.chunks...)
	vc.mu.Unlock()

	if len(chunks) == 0 {
		log.Warn().Str("session", sessionID).Msg("capture: video stopped with no chunks captured")
		s.logEvent(sessionID, "video.stop_no_chunks serial=%s duration=%s", vc.serial, time.Since(vc.startedAt))
		return ""
	}
	s.logEvent(sessionID, "video.stop chunks=%d", len(chunks))
	if len(chunks) == 1 {
		// Single chunk — just rename to the canonical filename so the
		// unified artifacts endpoint exposes it as "video.mp4".
		dir := filepath.Dir(chunks[0])
		final := filepath.Join(dir, "video.mp4")
		if err := os.Rename(chunks[0], final); err == nil {
			log.Info().Str("session", sessionID).Str("path", final).Msg("capture: video saved (single chunk)")
			return final
		}
		return chunks[0]
	}

	// Multi-chunk → try ffmpeg concat. Format is literal "file 'path'\n" lines.
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Warn().Str("session", sessionID).Int("chunks", len(chunks)).Msg("capture: ffmpeg not found — leaving chunks un-merged")
		return chunks[len(chunks)-1] // return latest chunk as best-guess
	}

	dir := filepath.Dir(chunks[0])
	listPath := filepath.Join(dir, ".concat.txt")
	var lines []byte
	for _, c := range chunks {
		lines = append(lines, []byte(fmt.Sprintf("file '%s'\n", c))...)
	}
	if err := os.WriteFile(listPath, lines, 0644); err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("capture: write concat list failed")
		return chunks[len(chunks)-1]
	}
	defer os.Remove(listPath)

	final := filepath.Join(dir, "video.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	concatCmd := exec.CommandContext(ctx, ffmpeg, "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", final)
	if out, err := concatCmd.CombinedOutput(); err != nil {
		log.Warn().Err(err).Str("session", sessionID).Str("ffmpeg_out", string(out)).Msg("capture: ffmpeg concat failed — keeping chunks")
		return chunks[len(chunks)-1]
	}

	// Merge succeeded — clean up per-chunk files.
	for _, c := range chunks {
		_ = os.Remove(c)
	}
	log.Info().Str("session", sessionID).Int("chunks", len(chunks)).Str("path", final).Msg("capture: video stitched")
	return final
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

// StartNetwork spins up a mitmdump proxy on a per-session port and
// points the emulator's global HTTP proxy at it so all traffic gets
// logged to a HAR file. HTTPS decrypt requires the user to have
// trusted the mitmproxy CA on the emulator image — we don't auto-
// install it because it requires root remount + reboot on most
// current API levels; unencrypted traffic is captured regardless,
// and HTTPS requests show up as CONNECT lines.
//
// No-op + clear warning when `mitmdump` isn't on PATH — we prefer
// failing the capability gracefully over failing the session.
func (s *Service) StartNetwork(sessionID, serial string) error {
	s.mu.Lock()
	if _, ok := s.networks[sessionID]; ok {
		s.mu.Unlock()
		return nil // idempotent
	}
	s.mu.Unlock()

	mitmdump, err := exec.LookPath("mitmdump")
	if err != nil {
		log.Warn().Str("session", sessionID).Msg("capture: mitmdump not found on PATH — install with `brew install mitmproxy` to enable network capture")
		return fmt.Errorf("mitmdump not found: %w", err)
	}

	dir := filepath.Join(s.dataDir, "artifacts", sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir artifacts: %w", err)
	}
	harPath := filepath.Join(dir, "network.har")

	s.portMu.Lock()
	port := s.nextProxyPort
	s.nextProxyPort++
	if s.nextProxyPort > 8999 {
		s.nextProxyPort = 8889
	}
	s.portMu.Unlock()

	// Start mitmdump with the built-in HAR save addon (requires
	// mitmproxy >=9; older versions need --set hardump=…). We pass
	// both forms and let mitmdump pick the one it understands.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, mitmdump,
		"--listen-host", "0.0.0.0",
		"--listen-port", fmt.Sprintf("%d", port),
		"--set", fmt.Sprintf("hardump=%s", harPath),
		"-q", // quiet — write to file, not stdout
	)
	// Route stderr so startup failures land in our log.
	cmd.Stderr = newLineWriter(func(line string) {
		log.Debug().Str("session", sessionID).Str("mitm", line).Msg("capture: mitmdump")
	})
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start mitmdump: %w", err)
	}

	// Give mitmdump ~1s to bind before we point the emulator at it.
	time.Sleep(1 * time.Second)

	// Emulator reaches the host via the magic 10.0.2.2 address.
	proxyAddr := fmt.Sprintf("10.0.2.2:%d", port)
	setCtx, setCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer setCancel()
	setCmd := exec.CommandContext(setCtx, s.adbPath, "-s", serial, "shell",
		"settings", "put", "global", "http_proxy", proxyAddr)
	if out, err := setCmd.CombinedOutput(); err != nil {
		_ = cmd.Process.Kill()
		cancel()
		return fmt.Errorf("set proxy: %w (%s)", err, string(out))
	}

	s.mu.Lock()
	s.networks[sessionID] = &networkCapture{
		cmd:       cmd,
		cancel:    cancel,
		serial:    serial,
		port:      port,
		harPath:   harPath,
		startedAt: time.Now(),
	}
	s.mu.Unlock()
	log.Info().Str("session", sessionID).Int("port", port).Str("har", harPath).Msg("capture: network started")
	return nil
}

// StopNetwork clears the emulator proxy, terminates mitmdump so it
// flushes the HAR, and returns the final HAR path. Best-effort;
// errors are logged and the broker's release proceeds regardless.
func (s *Service) StopNetwork(sessionID string) string {
	s.mu.Lock()
	nc, ok := s.networks[sessionID]
	if ok {
		delete(s.networks, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ""
	}

	// Remove the emulator's proxy setting first so in-flight requests
	// don't pile up once we kill mitmdump.
	clrCtx, clrCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = exec.CommandContext(clrCtx, s.adbPath, "-s", nc.serial, "shell",
		"settings", "delete", "global", "http_proxy").Run()
	clrCancel()

	// SIGINT → mitmdump flushes its HAR on graceful shutdown.
	if nc.cmd.Process != nil {
		_ = nc.cmd.Process.Signal(os.Interrupt)
	}
	done := make(chan struct{})
	go func() { nc.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		nc.cancel()
		<-done
	}

	if _, err := os.Stat(nc.harPath); err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("capture: network stopped but no HAR file was produced")
		return ""
	}
	log.Info().Str("session", sessionID).Str("path", nc.harPath).Msg("capture: network HAR saved")
	return nc.harPath
}

// StopAll stops every active capture for a session. Safe to call
// when nothing's active.
func (s *Service) StopAll(sessionID string) {
	s.StopVideo(sessionID)
	s.StopLogcat(sessionID)
	s.StopNetwork(sessionID)
}

// lineWriter invokes fn on every newline-terminated line written to
// it. Used to route mitmdump stderr into zerolog without buffering
// the entire stream.
type lineWriter struct {
	buf []byte
	fn  func(string)
}

func newLineWriter(fn func(string)) *lineWriter { return &lineWriter{fn: fn} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := -1
		for j, b := range w.buf {
			if b == '\n' {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if line != "" {
			w.fn(line)
		}
	}
	return len(p), nil
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
