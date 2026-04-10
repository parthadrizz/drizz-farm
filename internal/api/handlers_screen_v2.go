package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type screenV2Handlers struct {
	pool           *pool.Pool
	adb            *android.ADBClient
	sdk            *android.SDK
	scrcpyServer   string // path to scrcpy-server JAR
}

func newScreenV2Handlers(p *pool.Pool, adb *android.ADBClient, sdk *android.SDK) *screenV2Handlers {
	// Find scrcpy-server
	paths := []string{
		"/opt/homebrew/Cellar/scrcpy/3.2/share/scrcpy/scrcpy-server",
		"/opt/homebrew/share/scrcpy/scrcpy-server",
		"/usr/local/share/scrcpy/scrcpy-server",
		"/usr/share/scrcpy/scrcpy-server",
	}
	scrcpyPath := ""
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			scrcpyPath = p
			break
		}
	}
	if scrcpyPath != "" {
		log.Info().Str("path", scrcpyPath).Msg("screen: scrcpy-server found (H.264 streaming)")
	} else {
		log.Warn().Msg("screen: scrcpy-server not found, using screencap fallback")
	}

	return &screenV2Handlers{pool: p, adb: adb, sdk: sdk, scrcpyServer: scrcpyPath}
}

// StreamScreen streams emulator screen via scrcpy H.264 or screencap fallback.
// Browser decodes H.264 via WebCodecs API.
func (h *screenV2Handlers) StreamScreen(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	serial := h.findSerial(sessionID)
	if serial == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() { for { _, _, err := conn.ReadMessage(); if err != nil { cancel(); return } } }()

	// Try scrcpy first
	if h.scrcpyServer != "" {
		if h.streamViaScrcpy(ctx, conn, serial) {
			return
		}
	}

	// Fallback to screenrecord → raw H.264 pipe
	if h.streamViaScreenrecord(ctx, conn, serial) {
		return
	}

	// Last resort: screencap polling
	log.Info().Str("serial", serial).Msg("screen: using screencap fallback (slowest)")
	h.streamViaScreencap(ctx, conn, serial)
}

// streamViaScrcpy uses scrcpy-server for optimal H.264 streaming.
func (h *screenV2Handlers) streamViaScrcpy(ctx context.Context, conn *websocket.Conn, serial string) bool {
	// Push server to device
	pushCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "push", h.scrcpyServer, "/data/local/tmp/scrcpy-server.jar")
	if err := pushCmd.Run(); err != nil {
		log.Debug().Err(err).Msg("screen: scrcpy push failed")
		return false
	}

	// Find free port and forward
	port, err := getFreePort()
	if err != nil {
		return false
	}

	fwdCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "forward", fmt.Sprintf("tcp:%d", port), "localabstract:scrcpy")
	if err := fwdCmd.Run(); err != nil {
		return false
	}
	defer exec.Command(h.sdk.ADBPath(), "-s", serial, "forward", "--remove", fmt.Sprintf("tcp:%d", port)).Run()

	// Start scrcpy-server on device
	serverCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
		"CLASSPATH=/data/local/tmp/scrcpy-server.jar",
		"app_process", "/", "com.genymobile.scrcpy.Server",
		"3.2",
		"tunnel_forward=true",
		"video=true", "audio=false", "control=false",
		"max_size=720", "video_bit_rate=2000000", "max_fps=30",
		"video_codec=h264", "send_frame_meta=false",
	)
	if err := serverCmd.Start(); err != nil {
		return false
	}
	defer serverCmd.Process.Kill()

	// Wait for server, then connect
	time.Sleep(1500 * time.Millisecond)

	videoConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		log.Debug().Err(err).Msg("screen: scrcpy connect failed")
		return false
	}
	defer videoConn.Close()

	// Send codec header so browser knows it's H.264
	conn.WriteMessage(websocket.TextMessage, []byte(`{"codec":"h264","width":720,"height":1600}`))

	log.Info().Str("serial", serial).Msg("screen: scrcpy H.264 streaming started")

	// Pipe H.264 NAL units to WebSocket
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return true
		default:
		}
		videoConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := videoConn.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Debug().Err(err).Msg("screen: scrcpy read done")
			}
			return true
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
			return true
		}
	}
}

// streamViaScreenrecord uses Android's built-in screenrecord H.264 output.
func (h *screenV2Handlers) streamViaScreenrecord(ctx context.Context, conn *websocket.Conn, serial string) bool {
	cmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
		"screenrecord", "--output-format=h264", "--size", "720x1600", "--bit-rate", "2000000", "-")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	defer cmd.Process.Kill()

	conn.WriteMessage(websocket.TextMessage, []byte(`{"codec":"h264","width":720,"height":1600}`))

	log.Info().Str("serial", serial).Msg("screen: screenrecord H.264 streaming started")

	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return true
		default:
		}
		n, err := stdout.Read(buf)
		if err != nil {
			return true
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
			return true
		}
	}
}

// streamViaScreencap is the slow PNG fallback.
func (h *screenV2Handlers) streamViaScreencap(ctx context.Context, conn *websocket.Conn, serial string) {
	conn.WriteMessage(websocket.TextMessage, []byte(`{"codec":"png"}`))

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			png, err := h.adb.Screencap(ctx, serial)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, png); err != nil {
				return
			}
		}
	}
}

func (h *screenV2Handlers) findSerial(id string) string {
	if h.pool == nil { return "" }
	for _, inst := range h.pool.Status().Instances {
		if inst.SessionID == id || inst.ID == id {
			return inst.Serial
		}
	}
	if inst, ok := h.pool.GetInstance(id); ok && inst.Device != nil {
		return inst.Device.Serial()
	}
	return ""
}

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil { return 0, err }
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
