package api

import (
	"context"
	"net/http"
	"os/exec"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins (LAN)
}

type screenHandlers struct {
	pool *pool.Pool
	adb  *android.ADBClient
	sdk  *android.SDK
}

// StreamScreen handles WS /api/v1/sessions/:id/screen
// Streams PNG frames of the emulator screen (fallback).
func (h *screenHandlers) StreamScreen(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	status := h.pool.Status()
	var serial string
	for _, inst := range status.Instances {
		if inst.SessionID == sessionID || inst.ID == sessionID {
			serial = inst.Serial
			break
		}
	}
	if serial == "" {
		if inst, ok := h.pool.GetInstance(sessionID); ok && inst.Device != nil {
			serial = inst.Device.Serial()
		}
	}
	if serial == "" {
		http.Error(w, "session or instance not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("screen: websocket upgrade failed")
		return
	}
	defer conn.Close()

	log.Info().Str("serial", serial).Str("session", sessionID).Msg("screen: streaming started")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		for { _, _, err := conn.ReadMessage(); if err != nil { cancel(); return } }
	}()

	log.Info().Str("serial", serial).Msg("screen: streaming started (PNG)")

	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("serial", serial).Msg("screen: streaming stopped")
			return
		case <-ticker.C:
			png, err := h.adb.Screencap(ctx, serial)
			if err != nil { continue }
			if conn.WriteMessage(websocket.BinaryMessage, png) != nil { return }
		}
	}
}

// tryScreenrecord streams H.264 via Android's built-in screenrecord.
func (h *screenHandlers) tryScreenrecord(ctx context.Context, conn *websocket.Conn, serial string) bool {
	cmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
		"screenrecord", "--output-format=h264", "--size", "480x1066", "--bit-rate", "1000000", "-")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Debug().Err(err).Msg("screen: screenrecord pipe failed")
		return false
	}
	if err := cmd.Start(); err != nil {
		log.Debug().Err(err).Msg("screen: screenrecord start failed")
		return false
	}

	conn.WriteMessage(websocket.TextMessage, []byte(`{"codec":"h264","width":480,"height":1066}`))

	log.Info().Str("serial", serial).Msg("screen: H.264 streaming via screenrecord")

	buf := make([]byte, 32768)
	for {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return true
		default:
		}
		n, readErr := stdout.Read(buf)
		if n > 0 {
			if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				cmd.Process.Kill()
				return true
			}
		}
		if readErr != nil {
			cmd.Process.Kill()
			cmd.Wait()
			cmd = exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
				"screenrecord", "--output-format=h264", "--size", "480x1066", "--bit-rate", "1000000", "-")
			stdout, err = cmd.StdoutPipe()
			if err != nil { return true }
			if err = cmd.Start(); err != nil { return true }
			log.Debug().Str("serial", serial).Msg("screen: screenrecord restarted (3-min limit)")
		}
	}
}

// StreamLogcat handles WS /api/v1/sessions/:id/logcat
func (h *screenHandlers) StreamLogcat(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	serial := ""
	for _, inst := range h.pool.Status().Instances {
		if inst.SessionID == sessionID || inst.ID == sessionID {
			serial = inst.Serial
			break
		}
	}
	if serial == "" {
		if inst, ok := h.pool.GetInstance(sessionID); ok && inst.Device != nil {
			serial = inst.Device.Serial()
		}
	}
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

	go func() {
		for { _, _, err := conn.ReadMessage(); if err != nil { cancel(); return } }
	}()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			output, err := h.adb.Shell(ctx, serial, "logcat -d -t 20 -v brief")
			if err != nil {
				continue
			}
			if output != "" {
				if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(output)); writeErr != nil {
					return
				}
			}
		}
	}
}

// SendInput handles WS /api/v1/sessions/:id/input
// Receives touch/key events from browser and forwards to emulator.
// Uses exec-out for each command (fast, no PTY overhead).
// Serialized via channel so only one command runs at a time.
func (h *screenHandlers) SendInput(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	status := h.pool.Status()
	var serial string
	for _, inst := range status.Instances {
		if inst.SessionID == sessionID || inst.ID == sessionID {
			serial = inst.Serial
			break
		}
	}
	if serial == "" {
		if inst, ok := h.pool.GetInstance(sessionID); ok && inst.Device != nil {
			serial = inst.Device.Serial()
		}
	}
	if serial == "" {
		http.Error(w, "session or instance not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("input: websocket upgrade failed")
		return
	}
	defer conn.Close()

	log.Info().Str("serial", serial).Str("session", sessionID).Msg("input: relay started")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Serialized command queue — one at a time, drop stale
	cmdCh := make(chan string, 4)
	go func() {
		adbPath := h.sdk.ADBPath()
		for {
			select {
			case <-ctx.Done():
				return
			case cmd := <-cmdCh:
				// Use exec-out instead of shell — no PTY, less overhead
				c := exec.CommandContext(ctx, adbPath, "-s", serial, "shell", cmd)
				c.Run()
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		cmd := string(msg)
		if len(cmd) == 0 {
			continue
		}

		var adbCmd string
		if len(cmd) > 4 && cmd[:4] == "tap " {
			adbCmd = "input tap " + cmd[4:]
		} else if len(cmd) > 6 && cmd[:6] == "swipe " {
			adbCmd = "input swipe " + cmd[6:]
		} else if len(cmd) > 5 && cmd[:5] == "text " {
			adbCmd = "input text '" + cmd[5:] + "'"
		} else if len(cmd) > 4 && cmd[:4] == "key " {
			adbCmd = "input keyevent " + cmd[4:]
		} else if cmd == "home" {
			adbCmd = "input keyevent 3"
		} else if cmd == "back" {
			adbCmd = "input keyevent 4"
		} else if cmd == "recent" {
			adbCmd = "input keyevent 187"
		} else {
			continue
		}

		// Non-blocking: drop oldest if full
		select {
		case cmdCh <- adbCmd:
		default:
			select {
			case <-cmdCh:
			default:
			}
			select {
			case cmdCh <- adbCmd:
			default:
			}
		}
	}
}
