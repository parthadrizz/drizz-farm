package api

// WebDriver (W3C) compatibility endpoint — lets Appium clients treat
// drizz-farm as a drop-in Appium hub.
//
//   POST   /wd/hub/session                  — create session + start Appium
//   GET    /wd/hub/session/{sid}             — fetch status via the underlying Appium server
//   DELETE /wd/hub/session/{sid}             — release drizz session (stops captures, returns emu)
//   *      /wd/hub/session/{sid}/*           — transparent reverse-proxy to that session's Appium URL
//
// Capabilities: standard W3C `alwaysMatch` / `firstMatch` are forwarded
// verbatim to Appium. Anything prefixed `drizz:*` is stripped out and
// mapped to our session-create body:
//
//   drizz:profile           -> profile
//   drizz:device_id         -> device_id
//   drizz:avd_name          -> avd_name
//   drizz:record_video      -> capabilities.record_video
//   drizz:capture_logcat    -> capabilities.capture_logcat
//   drizz:capture_screenshots -> capabilities.capture_screenshots
//   drizz:capture_network   -> capabilities.capture_network
//   drizz:retention_hours   -> capabilities.retention_hours
//   drizz:timeout_minutes   -> timeout_minutes
//
// We key the session ID off whatever Appium returned (clients then see
// the Appium-issued ID on all subsequent calls) and maintain a local
// map Appium-sid → drizz-sid so DELETE can find the right drizz
// session to release.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/session"
)

type appiumCompatHandlers struct {
	broker *session.Broker

	// appiumSID → drizzSID map. Used so DELETE /wd/hub/session/{appiumSid}
	// can find the drizz session to release.
	mu         sync.RWMutex
	sidMapping map[string]string
}

func newAppiumCompatHandlers(b *session.Broker) *appiumCompatHandlers {
	return &appiumCompatHandlers{
		broker:     b,
		sidMapping: map[string]string{},
	}
}

// ---- GET /wd/hub/status + /wd/hub/sessions --------------------------
//
// Some Appium clients (notably appium-java-client's AppiumDriver,
// certain node.js clients) probe `/wd/hub/status` at driver init to
// confirm the server is an Appium-compatible hub. Without this,
// drivers print a warning + sometimes hard-fail. We return a minimal
// W3C-shaped status so those probes succeed; the real Appium server
// behind a session is still reached via the per-session proxy below.

func (h *appiumCompatHandlers) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"value": map[string]any{
			"ready":   true,
			"message": "drizz-farm Appium-compat ready",
			"build":   map[string]any{"version": "0.1.22"},
		},
	})
}

// Sessions lists active sessions in the WebDriver-style wrapper. Some
// test orchestrators call this to discover in-flight sessions (e.g.
// for cleanup after a crash). Returns only sessions we're tracking
// via our sid mapping, shaped like a WD GET /sessions response.
func (h *appiumCompatHandlers) Sessions(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	sids := make([]map[string]any, 0, len(h.sidMapping))
	for appiumSID, drizzSID := range h.sidMapping {
		sids = append(sids, map[string]any{
			"id":           appiumSID,
			"drizz_id":     drizzSID,
			"capabilities": map[string]any{},
		})
	}
	h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"value": sids})
}

// ---- POST /wd/hub/session -------------------------------------------

type wdCreateReq struct {
	Capabilities struct {
		AlwaysMatch map[string]any   `json:"alwaysMatch"`
		FirstMatch  []map[string]any `json:"firstMatch"`
	} `json:"capabilities"`
	// Legacy JSON-wire protocol. We accept it but re-read via the
	// alwaysMatch mirror if present so the create path has one shape.
	DesiredCapabilities map[string]any `json:"desiredCapabilities,omitempty"`
}

func (h *appiumCompatHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req wdCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		wdError(w, 400, "invalid session body: "+err.Error())
		return
	}
	caps := pickCaps(&req)
	drizzReq, cleanCaps := extractDrizzCaps(caps)

	// 1. Create the drizz session — allocator + capture wiring.
	sess, err := h.broker.Create(r.Context(), drizzReq)
	if err != nil {
		wdError(w, 500, "drizz allocate: "+err.Error())
		return
	}
	if sess.Connection.AppiumURL == "" {
		// Release so we don't leak a device.
		_ = h.broker.Release(r.Context(), sess.ID)
		wdError(w, 500, "drizz session has no appium_url — Appium server failed to start")
		return
	}

	// 2. Forward the original W3C capabilities (minus drizz:*) to the
	//    per-session Appium server. Appium responds with its own
	//    session id which we'll use for all subsequent routing.
	forwardBody := map[string]any{
		"capabilities": map[string]any{
			"alwaysMatch": cleanCaps,
			"firstMatch":  []map[string]any{{}},
		},
	}
	bodyBytes, _ := json.Marshal(forwardBody)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	upstream := strings.TrimRight(sess.Connection.AppiumURL, "/") + "/session"
	req2, _ := http.NewRequestWithContext(ctx, "POST", upstream, bytes.NewReader(bodyBytes))
	req2.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		_ = h.broker.Release(context.Background(), sess.ID)
		wdError(w, 502, "appium upstream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = h.broker.Release(context.Background(), sess.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// 3. Pull the Appium-issued session id out of the response + stash
	//    the mapping so future requests on that id can find our session.
	appiumSID := parseWDSessionID(respBody)
	if appiumSID == "" {
		_ = h.broker.Release(context.Background(), sess.ID)
		wdError(w, 500, "appium returned no sessionId")
		return
	}
	h.mu.Lock()
	h.sidMapping[appiumSID] = sess.ID
	h.mu.Unlock()
	log.Info().
		Str("appium_sid", appiumSID).
		Str("drizz_sid", sess.ID).
		Str("appium_url", sess.Connection.AppiumURL).
		Msg("appium-compat: session created")

	// Pass Appium's response through verbatim — clients see the
	// standard WebDriver create response shape.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// ---- Proxy for anything under /wd/hub/session/{sid}/* ----------------

func (h *appiumCompatHandlers) Proxy(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	drizzID, ok := h.lookup(sid)
	if !ok {
		wdError(w, 404, "unknown session "+sid)
		return
	}
	sess, err := h.broker.Get(drizzID)
	if err != nil || sess.Connection.AppiumURL == "" {
		wdError(w, 404, "session has no appium url: "+sid)
		return
	}

	// Build upstream URL: AppiumURL + the full path (drops /wd/hub prefix).
	// Example: GET /wd/hub/session/abc/screenshot → {AppiumURL}/session/abc/screenshot
	upstreamPath := strings.TrimPrefix(r.URL.Path, "/wd/hub")
	if upstreamPath == "" {
		upstreamPath = "/"
	}
	upstream := strings.TrimRight(sess.Connection.AppiumURL, "/") + upstreamPath
	if r.URL.RawQuery != "" {
		upstream += "?" + r.URL.RawQuery
	}

	// Stream the body so screenshot/page-source responses don't buffer
	// the whole PNG twice. 60s per-request is generous — real Appium
	// commands return in milliseconds.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	req2, err := http.NewRequestWithContext(ctx, r.Method, upstream, r.Body)
	if err != nil {
		wdError(w, 500, "proxy request build: "+err.Error())
		return
	}
	for k, vs := range r.Header {
		// Drop hop-by-hop headers; httputil.ReverseProxy's list is
		// authoritative but we only need the common ones here.
		if strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Keep-Alive") {
			continue
		}
		for _, v := range vs {
			req2.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		wdError(w, 502, "appium upstream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Special-case DELETE /wd/hub/session/{sid} — after Appium tears
	// down its session we also release the drizz session (stops
	// captures, returns the emulator, pulls video). Do this after we
	// finish relaying Appium's response so the client sees the
	// standard WebDriver DELETE return.
	isSessionDelete := r.Method == http.MethodDelete &&
		strings.TrimRight(upstreamPath, "/") == "/session/"+sid

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	if isSessionDelete {
		// Best-effort: a release failure here shouldn't roll back the
		// 200 we already sent the client.
		_ = h.broker.Release(context.Background(), drizzID)
		h.mu.Lock()
		delete(h.sidMapping, sid)
		h.mu.Unlock()
		log.Info().Str("appium_sid", sid).Str("drizz_sid", drizzID).Msg("appium-compat: session deleted")
	}
}

// ---- Helpers --------------------------------------------------------

func (h *appiumCompatHandlers) lookup(appiumSID string) (string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	v, ok := h.sidMapping[appiumSID]
	return v, ok
}

// pickCaps returns the best-effort capabilities map — alwaysMatch wins,
// falls back to desiredCapabilities (JSON-wire) when W3C is absent.
func pickCaps(req *wdCreateReq) map[string]any {
	if len(req.Capabilities.AlwaysMatch) > 0 {
		return req.Capabilities.AlwaysMatch
	}
	if req.DesiredCapabilities != nil {
		return req.DesiredCapabilities
	}
	return map[string]any{}
}

// extractDrizzCaps strips `drizz:*` keys out of the caps map and
// translates them into a session.CreateSessionRequest. The remaining
// caps are forwarded to Appium as-is.
func extractDrizzCaps(caps map[string]any) (session.CreateSessionRequest, map[string]any) {
	out := session.CreateSessionRequest{
		Source:       "appium-compat",
		Capabilities: &session.SessionCapabilities{CaptureScreenshots: true}, // sensible default
	}
	clean := make(map[string]any, len(caps))
	for k, v := range caps {
		if !strings.HasPrefix(k, "drizz:") {
			clean[k] = v
			continue
		}
		switch k[len("drizz:"):] {
		case "profile":
			out.Profile = asString(v)
		case "device_id":
			out.DeviceID = asString(v)
		case "avd_name":
			out.AVDName = asString(v)
		case "timeout_minutes":
			out.TimeoutMin = asInt(v)
		case "record_video":
			out.Capabilities.RecordVideo = asBool(v)
		case "capture_logcat":
			out.Capabilities.CaptureLogcat = asBool(v)
		case "capture_screenshots":
			out.Capabilities.CaptureScreenshots = asBool(v)
		case "capture_network":
			out.Capabilities.CaptureNetwork = asBool(v)
		case "retention_hours":
			out.Capabilities.RetentionHours = asInt(v)
		}
	}
	// Derive platform from appium:platformName so we set a default
	// profile when the caller didn't pin one.
	if out.Profile == "" && out.DeviceID == "" && out.AVDName == "" {
		if pn, ok := clean["platformName"].(string); ok && strings.EqualFold(pn, "iOS") {
			out.Platform = "ios"
		} else {
			out.Platform = "android"
		}
	}
	return out, clean
}

// parseWDSessionID extracts `sessionId` or `value.sessionId` from an
// Appium create-session response.
func parseWDSessionID(body []byte) string {
	var w3c struct {
		Value struct {
			SessionID string `json:"sessionId"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &w3c); err == nil && w3c.Value.SessionID != "" {
		return w3c.Value.SessionID
	}
	var jsonwire struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(body, &jsonwire); err == nil {
		return jsonwire.SessionID
	}
	return ""
}

// wdError writes a minimal WebDriver-style error response so Appium
// clients get a shape they recognize instead of our generic
// ErrorResponse JSON.
func wdError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{
		"value": map[string]any{
			"error":      "session not created",
			"message":    msg,
			"stacktrace": "",
		},
	}
	_ = json.NewEncoder(w).Encode(body)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1"
	}
	return false
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		var out int
		fmt.Sscanf(n, "%d", &out)
		return out
	}
	return 0
}

// Silence unused import when url isn't referenced (kept for future use
// when we route by scheme).
var _ = url.Parse
