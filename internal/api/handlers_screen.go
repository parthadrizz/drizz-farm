package api

import (
	"context"
	"net/http"
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
}

// StreamScreen handles WS /api/v1/sessions/:id/screen
// Streams MJPEG frames of the emulator screen.
func (h *screenHandlers) StreamScreen(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	// Find the instance for this session
	status := h.pool.Status()
	var serial string
	for _, inst := range status.Instances {
		if inst.SessionID == sessionID || inst.ID == sessionID {
			serial = inst.Serial
			break
		}
	}

	// Also try matching by instance ID directly
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

	// Read messages from client (for control: fps, stop, etc.)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Stream screenshots at ~5fps (200ms interval)
	// This is the simple approach; scrcpy-based streaming comes later
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("serial", serial).Msg("screen: streaming stopped")
			return
		case <-ticker.C:
			png, err := h.adb.Screencap(ctx, serial)
			if err != nil {
				log.Debug().Err(err).Str("serial", serial).Msg("screen: screencap failed")
				continue
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, png); err != nil {
				log.Debug().Err(err).Msg("screen: write failed")
				return
			}
		}
	}
}

// SendInput handles WS /api/v1/sessions/:id/input
// Receives touch/key events from browser and forwards to emulator.
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

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Parse input command: "tap 500 800", "swipe 100 500 100 100 300", "text hello", "key 66"
		cmd := string(msg)
		if len(cmd) == 0 {
			continue
		}

		var adbCmd string
		// Simple protocol: first word is action
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

		if _, err := h.adb.Shell(r.Context(), serial, adbCmd); err != nil {
			log.Debug().Err(err).Str("cmd", adbCmd).Msg("input: command failed")
		}
	}
}
