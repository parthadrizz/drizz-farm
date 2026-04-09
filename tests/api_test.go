//go:build integration

package tests

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAPI_NodeHealth(t *testing.T) {
	data := APIGet(t, "/node/health")
	var health map[string]any
	json.Unmarshal(data, &health)

	if health["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", health["status"])
	}
	if health["node"] == nil {
		t.Error("expected node name")
	}
	if health["version"] == nil {
		t.Error("expected version")
	}
	if health["uptime"] == nil {
		t.Error("expected uptime")
	}

	// Check nested objects exist
	if health["pool"] == nil {
		t.Error("expected pool info in health")
	}
	if health["sessions"] == nil {
		t.Error("expected sessions info in health")
	}
	if health["resources"] == nil {
		t.Error("expected resources info in health")
	}
}

func TestAPI_PoolStatus(t *testing.T) {
	pool := GetPool(t)
	if pool.TotalCapacity < 1 {
		t.Errorf("expected capacity >= 1, got %d", pool.TotalCapacity)
	}
}

func TestAPI_PoolAvailable(t *testing.T) {
	data := APIGet(t, "/pool/available")
	var result map[string]any
	json.Unmarshal(data, &result)

	if _, ok := result["available"]; !ok {
		t.Error("expected 'available' field")
	}
}

func TestAPI_CORS(t *testing.T) {
	req, _ := http.NewRequest(http.MethodOptions, apiBase+"/pool", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}
}

func TestAPI_RootEndpoint(t *testing.T) {
	resp, err := http.Get("http://127.0.0.1:9401/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Root serves the embedded React dashboard (HTML)
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" && ct != "text/html" {
		t.Logf("root content-type: %s (expected HTML for dashboard)", ct)
	}
}
