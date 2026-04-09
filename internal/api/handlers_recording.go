package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type recordingHandlers struct {
	pool       *pool.Pool
	adb        *android.ADBClient
	sdk        *android.SDK
	dataDir    string
	mu         sync.Mutex
	recordings map[string]*activeRecording
}

type activeRecording struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	filePath  string
	devicePath string
	serial    string
	startedAt time.Time
}

func newRecordingHandlers(p *pool.Pool, adb *android.ADBClient, sdk *android.SDK, dataDir string) *recordingHandlers {
	return &recordingHandlers{
		pool:       p,
		adb:        adb,
		sdk:        sdk,
		dataDir:    dataDir,
		recordings: make(map[string]*activeRecording),
	}
}

func (h *recordingHandlers) findSerial(id string) (string, string) {
	for _, inst := range h.pool.Status().Instances {
		if inst.ID == id || inst.SessionID == id {
			return inst.Serial, inst.ID
		}
	}
	if inst, ok := h.pool.GetInstance(id); ok && inst.Device != nil {
		return inst.Device.Serial(), id
	}
	return "", ""
}

// Start handles POST /api/v1/sessions/:id/recording/start
func (h *recordingHandlers) Start(w http.ResponseWriter, r *http.Request) {
	serial, instID := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	h.mu.Lock()
	if _, exists := h.recordings[instID]; exists {
		h.mu.Unlock()
		JSON(w, 409, ErrorResponse{Error: "already_recording", Message: "already recording", Code: 409})
		return
	}

	dir := filepath.Join(h.dataDir, "artifacts", instID)
	os.MkdirAll(dir, 0755)
	filename := fmt.Sprintf("rec_%s.mp4", time.Now().Format("20060102_150405"))
	devicePath := "/sdcard/" + filename
	localPath := filepath.Join(dir, filename)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell", "screenrecord", "--time-limit", "180", devicePath)
	if err := cmd.Start(); err != nil {
		cancel()
		h.mu.Unlock()
		JSON(w, 500, ErrorResponse{Error: "recording_failed", Message: err.Error(), Code: 500})
		return
	}

	h.recordings[instID] = &activeRecording{
		cmd:        cmd,
		cancel:     cancel,
		filePath:   localPath,
		devicePath: devicePath,
		serial:     serial,
		startedAt:  time.Now(),
	}
	h.mu.Unlock()

	log.Info().Str("instance", instID).Str("file", filename).Msg("recording: started")
	JSON(w, 200, map[string]any{"status": "recording", "filename": filename})
}

// Stop handles POST /api/v1/sessions/:id/recording/stop
func (h *recordingHandlers) Stop(w http.ResponseWriter, r *http.Request) {
	_, instID := h.findSerial(chi.URLParam(r, "id"))

	h.mu.Lock()
	rec, exists := h.recordings[instID]
	if !exists {
		h.mu.Unlock()
		JSON(w, 404, ErrorResponse{Error: "not_recording", Message: "no recording in progress", Code: 404})
		return
	}
	delete(h.recordings, instID)
	h.mu.Unlock()

	rec.cancel()
	duration := time.Since(rec.startedAt)
	log.Info().Str("instance", instID).Dur("duration", duration).Msg("recording: stopped")

	// Pull file from device
	time.Sleep(1 * time.Second)
	h.adb.Pull(context.Background(), rec.serial, rec.devicePath, rec.filePath)
	h.adb.Shell(context.Background(), rec.serial, "rm "+rec.devicePath)

	JSON(w, 200, map[string]any{
		"status":   "stopped",
		"duration": duration.String(),
		"file":     filepath.Base(rec.filePath),
	})
}

// Download handles GET /api/v1/sessions/:id/recording/download
func (h *recordingHandlers) Download(w http.ResponseWriter, r *http.Request) {
	_, instID := h.findSerial(chi.URLParam(r, "id"))
	if instID == "" {
		instID = chi.URLParam(r, "id")
	}

	dir := filepath.Join(h.dataDir, "artifacts", instID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "no recordings", Code: 404})
		return
	}

	var latest string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".mp4" {
			latest = filepath.Join(dir, e.Name())
		}
	}
	if latest == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "no recordings", Code: 404})
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(latest)))
	http.ServeFile(w, r, latest)
}

// List handles GET /api/v1/sessions/:id/recordings
func (h *recordingHandlers) List(w http.ResponseWriter, r *http.Request) {
	_, instID := h.findSerial(chi.URLParam(r, "id"))
	if instID == "" {
		instID = chi.URLParam(r, "id")
	}

	dir := filepath.Join(h.dataDir, "artifacts", instID)
	entries, _ := os.ReadDir(dir)

	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil {
			files = append(files, map[string]any{
				"name": e.Name(),
				"size": info.Size(),
				"time": info.ModTime().Format(time.RFC3339),
			})
		}
	}

	h.mu.Lock()
	isRecording := h.recordings[instID] != nil
	h.mu.Unlock()

	JSON(w, 200, map[string]any{
		"files":     files,
		"recording": isRecording,
	})
}

// Screenshot handles POST /api/v1/sessions/:id/screenshot
func (h *recordingHandlers) Screenshot(w http.ResponseWriter, r *http.Request) {
	serial, instID := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	png, err := h.adb.Screencap(r.Context(), serial)
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "screenshot_failed", Message: err.Error(), Code: 500})
		return
	}

	// Save to artifacts
	dir := filepath.Join(h.dataDir, "artifacts", instID)
	os.MkdirAll(dir, 0755)
	filename := fmt.Sprintf("screenshot_%s.png", time.Now().Format("20060102_150405"))
	localPath := filepath.Join(dir, filename)
	os.WriteFile(localPath, png, 0644)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", filename))
	w.Write(png)
}

// GetLogcat handles GET /api/v1/sessions/:id/logcat/download — dumps logcat as text file
func (h *recordingHandlers) GetLogcat(w http.ResponseWriter, r *http.Request) {
	serial, _ := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	lines := r.URL.Query().Get("lines")
	if lines == "" {
		lines = "500"
	}

	output, err := h.adb.Shell(r.Context(), serial, fmt.Sprintf("logcat -d -t %s", lines))
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "logcat_failed", Message: err.Error(), Code: 500})
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=logcat.txt")
	w.Write([]byte(output))
}

// StartHAR handles POST /api/v1/sessions/:id/har/start — begins tcpdump capture
func (h *recordingHandlers) StartHAR(w http.ResponseWriter, r *http.Request) {
	serial, instID := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	// Start tcpdump on device
	devicePath := "/sdcard/capture.pcap"
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("nohup tcpdump -i any -w %s &", devicePath))

	dir := filepath.Join(h.dataDir, "artifacts", instID)
	os.MkdirAll(dir, 0755)

	log.Info().Str("instance", instID).Msg("har: tcpdump started")
	JSON(w, 200, map[string]any{"status": "capturing"})
}

// StopHAR handles POST /api/v1/sessions/:id/har/stop — stops tcpdump and pulls pcap
func (h *recordingHandlers) StopHAR(w http.ResponseWriter, r *http.Request) {
	serial, instID := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	// Kill tcpdump
	h.adb.Shell(r.Context(), serial, "pkill tcpdump")
	time.Sleep(500 * time.Millisecond)

	// Pull pcap
	dir := filepath.Join(h.dataDir, "artifacts", instID)
	filename := fmt.Sprintf("capture_%s.pcap", time.Now().Format("20060102_150405"))
	localPath := filepath.Join(dir, filename)
	h.adb.Pull(r.Context(), serial, "/sdcard/capture.pcap", localPath)
	h.adb.Shell(r.Context(), serial, "rm /sdcard/capture.pcap")

	log.Info().Str("instance", instID).Str("file", filename).Msg("har: capture saved")
	JSON(w, 200, map[string]any{"status": "stopped", "file": filename})
}

// DownloadHAR handles GET /api/v1/sessions/:id/har/download
func (h *recordingHandlers) DownloadHAR(w http.ResponseWriter, r *http.Request) {
	_, instID := h.findSerial(chi.URLParam(r, "id"))
	if instID == "" {
		instID = chi.URLParam(r, "id")
	}

	dir := filepath.Join(h.dataDir, "artifacts", instID)
	entries, _ := os.ReadDir(dir)

	var latest string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".pcap" {
			latest = filepath.Join(dir, e.Name())
		}
	}
	if latest == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "no captures", Code: 404})
		return
	}

	w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(latest)))
	http.ServeFile(w, r, latest)
}
