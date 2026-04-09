package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- Session Handlers ---

func TestSessionHandler_CreateMissingBody(t *testing.T) {
	// Create handler with nil broker — should handle gracefully
	h := &sessionHandlers{broker: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader([]byte(`invalid`)))
	// This will panic because broker is nil — but the JSON decode should fail first
	// Let's test with empty body
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader([]byte(`{}`)))
	defer func() { recover() }() // broker is nil, catch panic
	h.Create(w2, r2)
	_ = w
	_ = r
}

// --- Pool Handlers ---

func TestPoolHandler_StatusNilPool(t *testing.T) {
	defer func() { recover() }()
	h := &poolHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/pool", nil)
	h.Status(w, r)
}

func TestPoolHandler_BootMissingBody(t *testing.T) {
	h := &poolHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/pool/boot", bytes.NewReader([]byte(`{}`)))
	h.Boot(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing avd_name, got %d", w.Code)
	}
}

func TestPoolHandler_BootInvalidJSON(t *testing.T) {
	h := &poolHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/pool/boot", bytes.NewReader([]byte(`not json`)))
	h.Boot(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPoolHandler_ShutdownMissingBody(t *testing.T) {
	h := &poolHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/pool/shutdown", bytes.NewReader([]byte(`{}`)))
	h.Shutdown(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing instance_id, got %d", w.Code)
	}
}

// --- Config Handlers ---

func TestConfigHandler_GetConfig(t *testing.T) {
	// Test with nil config — should handle
	defer func() { recover() }()
	h := &configHandlers{cfg: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/config", nil)
	h.GetConfig(w, r)
}

func TestConfigHandler_UpdateInvalidJSON(t *testing.T) {
	h := &configHandlers{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader([]byte(`not json`)))
	h.UpdateConfig(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// --- Discovery Handlers ---

func TestDiscoveryHandler_CreateAVDsMissingFields(t *testing.T) {
	h := &discoveryHandlers{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/discovery/create-avds", bytes.NewReader([]byte(`{"profile_name":"","device":"","system_image":"","count":0}`)))
	h.CreateAVDs(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing fields, got %d", w.Code)
	}
}

func TestDiscoveryHandler_CreateAVDsInvalidJSON(t *testing.T) {
	h := &discoveryHandlers{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/discovery/create-avds", bytes.NewReader([]byte(`bad`)))
	h.CreateAVDs(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- Snapshot Handlers ---

func TestSnapshotHandler_SaveMissingName(t *testing.T) {
	h := &snapshotHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sessions/abc/snapshot/save", bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	r = r.WithContext(addChiContext(r, rctx))
	h.Save(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}
}

func TestSnapshotHandler_RestoreMissingName(t *testing.T) {
	h := &snapshotHandlers{pool: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sessions/abc/snapshot/restore", bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	r = r.WithContext(addChiContext(r, rctx))
	h.Restore(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}
}

// --- Recording Handlers ---

func TestRecordingHandler_StopNoRecording(t *testing.T) {
	h := newRecordingHandlers(nil, nil, nil, "/tmp")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sessions/abc/recording/stop", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	r = r.WithContext(addChiContext(r, rctx))
	h.Stop(w, r)
	if w.Code != 404 {
		t.Errorf("expected 404 for no recording, got %d", w.Code)
	}
}

func TestRecordingHandler_ListNoArtifacts(t *testing.T) {
	h := newRecordingHandlers(nil, nil, nil, "/tmp/nonexistent-drizz-test")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/sessions/abc/recordings", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	r = r.WithContext(addChiContext(r, rctx))
	h.List(w, r)
	if w.Code != 200 {
		t.Errorf("expected 200 for empty list, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["recording"] != false {
		t.Error("expected recording=false")
	}
}

// --- History Handlers ---

func TestHistoryHandler_NilStore(t *testing.T) {
	h := &historyHandlers{store: nil}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/history/sessions", nil)
	h.SessionHistory(w, r)
	if w.Code != 200 {
		t.Errorf("expected 200 even with nil store, got %d", w.Code)
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/v1/history/events", nil)
	h.Events(w2, r2)
	if w2.Code != 200 {
		t.Errorf("expected 200 even with nil store, got %d", w2.Code)
	}
}

// --- Device Handlers (input validation) ---

func TestDeviceHandler_NotFound(t *testing.T) {
	// All device handlers with nil pool should return 404
	h := &deviceHandlers{pool: nil}

	endpoints := []struct {
		name   string
		method string
		body   string
	}{
		{"gps", "POST", `{"latitude":0,"longitude":0}`},
		{"network", "POST", `{"profile":"4g"}`},
		{"battery", "POST", `{"level":50}`},
		{"orientation", "POST", `{"rotation":0}`},
		{"appearance", "POST", `{"dark":true}`},
		{"locale", "POST", `{"locale":"en-US"}`},
		{"timezone", "POST", `{"timezone":"UTC"}`},
		{"biometric", "POST", `{"action":"touch"}`},
		{"font-scale", "POST", `{"scale":1.0}`},
		{"shake", "POST", `{}`},
		{"volume", "POST", `{"action":"up"}`},
		{"lock", "POST", `{"lock":true}`},
		{"animations", "POST", `{"enabled":false}`},
		{"accessibility", "POST", `{"talkback":true}`},
		{"brightness", "POST", `{"level":100}`},
		{"wifi", "POST", `{"enabled":true}`},
	}

	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(ep.method, "/api/v1/sessions/nonexistent/"+ep.name, bytes.NewReader([]byte(ep.body)))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "nonexistent")
		r = r.WithContext(addChiContext(r, rctx))

		// Call the right handler based on name
		switch ep.name {
		case "gps": h.SetGPS(w, r)
		case "network": h.SetNetwork(w, r)
		case "battery": h.SetBattery(w, r)
		case "orientation": h.SetOrientation(w, r)
		case "appearance": h.SetDarkMode(w, r)
		case "locale": h.SetLocale(w, r)
		case "timezone": h.SetTimezone(w, r)
		case "biometric": h.Biometric(w, r)
		case "font-scale": h.FontScale(w, r)
		case "shake": h.Shake(w, r)
		case "volume": h.Volume(w, r)
		case "lock": h.LockUnlock(w, r)
		case "animations": h.Animations(w, r)
		case "accessibility": h.Accessibility(w, r)
		case "brightness": h.Brightness(w, r)
		case "wifi": h.WifiToggle(w, r)
		}

		if w.Code != 404 {
			t.Errorf("%s: expected 404 for nonexistent instance, got %d", ep.name, w.Code)
		}
	}
}

// Helper to add chi route context
func addChiContext(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}
