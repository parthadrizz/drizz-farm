//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestE2E_AllDeviceFeatures tests every single BrowserStack/LambdaTest parity
// feature on a real emulator. This is the "we can do everything they can" test.
func TestE2E_AllDeviceFeatures(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)
	inst := sess.InstanceID

	passed := 0
	failed := 0

	run := func(name string, path string, body string, expectKey string) {
		t.Helper()
		data := APIPost(t, fmt.Sprintf("/sessions/%s/%s", inst, path), body)
		var resp map[string]any
		json.Unmarshal(data, &resp)
		if resp["error"] != nil {
			t.Logf("  ✗ %-30s FAIL: %v", name, resp["message"])
			failed++
		} else if expectKey != "" && resp[expectKey] == nil {
			t.Logf("  ✗ %-30s FAIL: missing key '%s'", name, expectKey)
			failed++
		} else {
			t.Logf("  ✓ %-30s OK", name)
			passed++
		}
	}

	t.Log("=== DEVICE SIMULATION FEATURES ===")
	t.Log("")

	// GPS
	t.Log("--- GPS ---")
	run("GPS: San Francisco", "gps", `{"latitude":37.7749,"longitude":-122.4194}`, "status")
	run("GPS: Mumbai", "gps", `{"latitude":19.076,"longitude":72.8777}`, "status")
	run("GPS: Tokyo", "gps", `{"latitude":35.6762,"longitude":139.6503}`, "status")

	// Network
	t.Log("--- Network Simulation ---")
	run("Network: 2G", "network", `{"profile":"2g"}`, "status")
	run("Network: 3G", "network", `{"profile":"3g"}`, "status")
	run("Network: 4G", "network", `{"profile":"4g"}`, "status")
	run("Network: 5G", "network", `{"profile":"5g"}`, "status")
	run("Network: WiFi slow", "network", `{"profile":"wifi_slow"}`, "status")
	run("Network: WiFi fast", "network", `{"profile":"wifi_fast"}`, "status")
	run("Network: Offline", "network", `{"profile":"offline"}`, "status")
	run("Network: Restore", "network", `{"profile":"4g"}`, "status") // restore connectivity

	// Battery
	t.Log("--- Battery ---")
	run("Battery: 100% AC", "battery", `{"level":100,"charging":"ac"}`, "status")
	run("Battery: 10% none", "battery", `{"level":10,"charging":"none"}`, "status")
	run("Battery: 50% USB", "battery", `{"level":50,"charging":"usb"}`, "status")

	// Orientation
	t.Log("--- Orientation ---")
	run("Orientation: Portrait", "orientation", `{"rotation":0}`, "status")
	run("Orientation: Landscape L", "orientation", `{"rotation":1}`, "status")
	run("Orientation: Reverse", "orientation", `{"rotation":2}`, "status")
	run("Orientation: Landscape R", "orientation", `{"rotation":3}`, "status")
	run("Orientation: Reset", "orientation", `{"rotation":0}`, "status")

	// Appearance
	t.Log("--- Appearance ---")
	run("Dark Mode: On", "appearance", `{"dark":true}`, "status")
	run("Dark Mode: Off", "appearance", `{"dark":false}`, "status")

	// Locale
	t.Log("--- Locale ---")
	run("Locale: en-US", "locale", `{"locale":"en-US"}`, "status")
	run("Locale: ja-JP", "locale", `{"locale":"ja-JP"}`, "status")
	run("Locale: hi-IN", "locale", `{"locale":"hi-IN"}`, "status")
	run("Locale: ar-SA", "locale", `{"locale":"ar-SA"}`, "status")
	run("Locale: Reset", "locale", `{"locale":"en-US"}`, "status")

	// Timezone
	t.Log("--- Timezone ---")
	run("Timezone: NYC", "timezone", `{"timezone":"America/New_York"}`, "status")
	run("Timezone: Tokyo", "timezone", `{"timezone":"Asia/Tokyo"}`, "status")
	run("Timezone: Mumbai", "timezone", `{"timezone":"Asia/Kolkata"}`, "status")
	run("Timezone: Reset", "timezone", `{"timezone":"America/Los_Angeles"}`, "status")

	// Font Scale
	t.Log("--- Font Scale ---")
	run("Font: Small (0.85)", "font-scale", `{"scale":0.85}`, "status")
	run("Font: Normal (1.0)", "font-scale", `{"scale":1.0}`, "status")
	run("Font: Large (1.3)", "font-scale", `{"scale":1.3}`, "status")
	run("Font: Reset", "font-scale", `{"scale":1.0}`, "status")

	// Deep Link
	t.Log("--- Deep Link ---")
	run("Deeplink: Google", "deeplink", `{"url":"https://google.com"}`, "status")
	run("Deeplink: YouTube", "deeplink", `{"url":"https://youtube.com"}`, "status")
	time.Sleep(1 * time.Second) // let Chrome open

	// App Management
	t.Log("--- App Management ---")
	run("Clear Data: Chrome", "clear-data", `{"package":"com.android.chrome"}`, "status")
	run("Permissions: Grant Camera", "permissions", `{"package":"com.android.chrome","permission":"android.permission.CAMERA","grant":true}`, "status")
	run("Permissions: Revoke Camera", "permissions", `{"package":"com.android.chrome","permission":"android.permission.CAMERA","grant":false}`, "status")

	// Biometric
	t.Log("--- Biometric ---")
	run("Biometric: Touch", "biometric", `{"action":"touch"}`, "status")
	run("Biometric: Fail", "biometric", `{"action":"fail"}`, "status")

	// Sensors
	t.Log("--- Sensors ---")
	run("Sensor: Accelerometer", "sensor", `{"name":"acceleration","values":"0:9.8:0"}`, "status")
	run("Sensor: Gyroscope", "sensor", `{"name":"gyroscope","values":"0:0:0"}`, "status")
	run("Shake", "shake", `{}`, "status")

	// Clipboard
	t.Log("--- Clipboard ---")
	run("Clipboard: Set text", "clipboard", `{"text":"hello drizz-farm"}`, "status")

	// ADB Shell
	t.Log("--- ADB Shell ---")
	run("ADB: getprop model", "adb", `{"command":"getprop ro.product.model"}`, "output")
	run("ADB: list packages", "adb", `{"command":"pm list packages | head -5"}`, "output")
	run("ADB: dumpsys battery", "adb", `{"command":"dumpsys battery | head -3"}`, "output")
	run("ADB: screen brightness", "adb", `{"command":"settings get system screen_brightness"}`, "output")

	// Summary
	t.Log("")
	t.Log("=== RESULTS ===")
	t.Logf("  Passed: %d", passed)
	t.Logf("  Failed: %d", failed)
	t.Logf("  Total:  %d", passed+failed)

	if failed > 0 {
		t.Errorf("%d features failed", failed)
	}
}
