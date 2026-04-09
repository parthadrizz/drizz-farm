//go:build integration

package tests

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAPI_PoolBoot(t *testing.T) {
	// Boot a specific AVD
	resp := APIPost(t, "/pool/boot", `{"avd_name":"drizz_api34_ext8_play_0"}`)
	var result map[string]any
	json.Unmarshal(resp, &result)

	if result["status"] != "booting" && result["error"] == nil {
		t.Logf("boot response: %v", result)
	}
}

func TestAPI_Config(t *testing.T) {
	data := APIGet(t, "/config")
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	if cfg["Pool"] == nil {
		t.Error("expected Pool in config")
	}
	if cfg["Node"] == nil {
		t.Error("expected Node in config")
	}
}

func TestAPI_ConfigRaw(t *testing.T) {
	resp, err := http.Get(apiBase + "/config/raw")
	if err != nil {
		t.Fatalf("GET /config/raw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/yaml" {
		t.Errorf("expected text/yaml, got %s", resp.Header.Get("Content-Type"))
	}
}

func TestAPI_Discovery_SystemImages(t *testing.T) {
	data := APIGet(t, "/discovery/system-images")
	var result map[string]any
	json.Unmarshal(data, &result)

	images, ok := result["images"].([]any)
	if !ok {
		t.Fatal("expected images array")
	}
	if len(images) < 1 {
		t.Error("expected at least 1 system image")
	}
}

func TestAPI_Discovery_Devices(t *testing.T) {
	data := APIGet(t, "/discovery/devices")
	var result map[string]any
	json.Unmarshal(data, &result)

	devices, ok := result["devices"].([]any)
	if !ok {
		t.Fatal("expected devices array")
	}
	if len(devices) < 1 {
		t.Error("expected at least 1 device definition")
	}
}

func TestAPI_Discovery_AVDs(t *testing.T) {
	data := APIGet(t, "/discovery/avds")
	var result map[string]any
	json.Unmarshal(data, &result)

	avds, ok := result["avds"].([]any)
	if !ok {
		t.Fatal("expected avds array")
	}
	if len(avds) < 1 {
		t.Error("expected at least 1 AVD")
	}
}

func TestAPI_History_Sessions(t *testing.T) {
	data := APIGet(t, "/history/sessions")
	var result map[string]any
	json.Unmarshal(data, &result)

	if _, ok := result["sessions"]; !ok {
		t.Error("expected sessions key")
	}
}

func TestAPI_History_Events(t *testing.T) {
	data := APIGet(t, "/history/events")
	var result map[string]any
	json.Unmarshal(data, &result)

	if _, ok := result["events"]; !ok {
		t.Error("expected events key")
	}
}

