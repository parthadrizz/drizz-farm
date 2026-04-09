//go:build integration

package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// --- Sprint 3: Device Simulation ---

func TestDeviceSim_GPS(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/gps", `{"latitude":37.7749,"longitude":-122.4194}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	if r["status"] != "set" {
		t.Errorf("expected status 'set', got %v", r["status"])
	}
}

func TestDeviceSim_DarkMode(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/appearance", `{"dark":true}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	if r["status"] != "set" {
		t.Errorf("expected status 'set', got %v", r["status"])
	}
}

func TestDeviceSim_Orientation(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/orientation", `{"rotation":1}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	if r["status"] != "set" {
		t.Errorf("expected status 'set', got %v", r["status"])
	}

	// Reset to portrait
	APIPost(t, "/sessions/"+sess.InstanceID+"/orientation", `{"rotation":0}`)
}

func TestDeviceSim_Locale(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/locale", `{"locale":"ja-JP"}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	if r["status"] != "set" {
		t.Errorf("expected status 'set', got %v", r["status"])
	}
}

func TestDeviceSim_Deeplink(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/deeplink", `{"url":"https://google.com"}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	if r["status"] != "opened" {
		t.Errorf("expected status 'opened', got %v", r["status"])
	}
}

func TestDeviceSim_ADBShell(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIPost(t, "/sessions/"+sess.InstanceID+"/adb", `{"command":"getprop ro.product.model"}`)
	var r map[string]any
	json.Unmarshal(data, &r)
	output, _ := r["output"].(string)
	if output == "" {
		t.Error("expected non-empty ADB output")
	}
	t.Logf("device model: %s", output)
}

// --- Sprint 4: Recording + Artifacts ---

func TestRecording_Screenshot(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	resp, err := http.Post(apiBase+"/sessions/"+sess.InstanceID+"/screenshot", "application/json", nil)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("expected image/png, got %s", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1000 {
		t.Errorf("screenshot too small: %d bytes", len(body))
	}
}

func TestRecording_VideoStartStop(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	// Start
	data := APIPost(t, "/sessions/"+sess.InstanceID+"/recording/start", "")
	var startR map[string]any
	json.Unmarshal(data, &startR)
	if startR["status"] != "recording" {
		t.Errorf("expected 'recording', got %v", startR["status"])
	}

	// Record for 3 seconds
	time.Sleep(3 * time.Second)

	// Stop
	data = APIPost(t, "/sessions/"+sess.InstanceID+"/recording/stop", "")
	var stopR map[string]any
	json.Unmarshal(data, &stopR)
	if stopR["status"] != "stopped" {
		t.Errorf("expected 'stopped', got %v", stopR["status"])
	}
}

func TestRecording_ListRecordings(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIGet(t, "/sessions/"+sess.InstanceID+"/recordings")
	var r map[string]any
	json.Unmarshal(data, &r)
	// Should have files key even if empty
	if _, ok := r["files"]; !ok {
		t.Error("expected 'files' key in response")
	}
}

func TestRecording_LogcatDownload(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	resp, err := http.Get(apiBase + "/sessions/" + sess.InstanceID + "/logcat/download?lines=50")
	if err != nil {
		t.Fatalf("logcat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("expected text/plain, got %s", resp.Header.Get("Content-Type"))
	}
}

