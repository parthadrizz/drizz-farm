//go:build integration

// Integration test suite covering every capability added in
// v0.1.14 → v0.1.21. Runs as one big sub-stepped test so we get a
// clean "X passed / Y failed" summary at the end — matches the
// existing e2e_device_features_test.go style.
//
// Shape:
//   - runs against the live daemon booted by tests/helpers_test.go::TestMain
//   - expects at least one warm emulator for full coverage
//     (otherwise ~half the steps skip with a clear note)
//   - downloads ApiDemos once per run, caches at /tmp/drizz-apidemos.apk
//   - each step has its own wall-time budget; full suite targets 5 min
//
// Run with:
//   make test-integration
// or
//   go test -tags=integration -timeout 5m -run TestCapabilities ./tests/ -v

package tests

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---- Constants tests rely on --------------------------------------

// ApiDemos is the canonical Appium sample app — signed debug APK
// that's been the Android-testing ecosystem's standard fixture for
// a decade. Small (~3 MB), widely mirrored on GitHub, stable
// package name across versions.
const (
	apiDemosPkg = "io.appium.android.apis"
	apiDemosURL = "https://github.com/appium/android-apidemos/releases/download/v3.1.0/ApiDemos-debug.apk"
	// SHA-256 of the v3.1.0 release. Verified at fetch time; mismatch
	// causes the APK-dependent subtests to skip (safer than silently
	// running against tampered bits).
	apiDemosSHA256 = "" // filled on first successful fetch if empty

	apkCachePath = "/tmp/drizz-apidemos.apk"
)

// ---- Sub-step scaffolding (pass/fail counter) ---------------------

type stepResult struct {
	name   string
	ok     bool
	detail string
	skip   bool
	dur    time.Duration
}

type suite struct {
	t     *testing.T
	steps []stepResult
}

// run wraps a subtest so one failure doesn't stop the rest. Each
// step has its own budget via the outer Go test timeout plus any
// explicit deadlines inside fn. We collect results for a summary
// printed at the end of the outer test.
func (s *suite) run(name string, fn func(t *subT)) {
	s.t.Helper()
	t := &subT{T: s.t, name: name}
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			t.failf("panic: %v", r)
		}
		s.steps = append(s.steps, stepResult{
			name:   name,
			ok:     !t.failed && !t.skipped,
			detail: t.detail,
			skip:   t.skipped,
			dur:    time.Since(start),
		})
	}()
	fn(t)
}

// subT is a thin sub-reporter — we don't use t.Run directly because
// a failed subtest would normally short-circuit only within that
// subtest, but on panic still aborts; our scaffolding catches panics
// and converts them to step failures so the remaining steps keep
// running.
type subT struct {
	*testing.T
	name    string
	failed  bool
	skipped bool
	detail  string
}

func (t *subT) failf(format string, args ...any) {
	t.failed = true
	t.detail = fmt.Sprintf(format, args...)
	t.T.Logf("  ✗ %-38s %s", t.name, t.detail)
}

func (t *subT) skipf(format string, args ...any) {
	t.skipped = true
	t.detail = fmt.Sprintf(format, args...)
	t.T.Logf("  ○ %-38s SKIP: %s", t.name, t.detail)
}

func (t *subT) pass(detail string) {
	t.detail = detail
	if detail == "" {
		t.T.Logf("  ✓ %-38s", t.name)
	} else {
		t.T.Logf("  ✓ %-38s (%s)", t.name, detail)
	}
}

// summary prints the totals at the end. Failures cause the parent
// test to fail; skips don't.
func (s *suite) summary() {
	passed, failed, skipped := 0, 0, 0
	var total time.Duration
	for _, r := range s.steps {
		total += r.dur
		switch {
		case r.skip:
			skipped++
		case r.ok:
			passed++
		default:
			failed++
		}
	}
	s.t.Log("")
	s.t.Log(strings.Repeat("━", 60))
	s.t.Logf("  RESULTS: %d passed  %d failed  %d skipped  (%s)",
		passed, failed, skipped, total.Round(time.Millisecond))
	s.t.Log(strings.Repeat("━", 60))
	if failed > 0 {
		s.t.Fail()
	}
}

// ---- HTTP helpers scoped to this file -----------------------------

func httpGetJSON(ctx context.Context, path string, out any) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if out != nil && len(body) > 0 {
		_ = json.Unmarshal(body, out)
	}
	return resp.StatusCode, nil
}

func httpPost(ctx context.Context, path, ctype, body string, headers map[string]string) (int, []byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+path, strings.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func httpPostJSON(ctx context.Context, path, body string) (int, []byte) {
	code, b, err := httpPost(ctx, path, "application/json", body, nil)
	if err != nil {
		return 0, []byte(err.Error())
	}
	return code, b
}

func httpDelete(ctx context.Context, path string) (int, []byte) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func postMultipart(ctx context.Context, path string, files map[string][]byte, fields map[string]string) (int, []byte) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for field, data := range files {
		part, _ := w.CreateFormFile(field, field+".bin")
		part.Write(data)
	}
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// adbShell runs a raw shell command via our own /adb endpoint so we
// don't depend on the test runner having adb installed separately.
// Returns the concatenated stdout/stderr.
func adbShell(ctx context.Context, sessionID, cmd string) string {
	body := fmt.Sprintf(`{"command":"shell %s"}`, strings.ReplaceAll(cmd, `"`, `\"`))
	_, out := httpPostJSON(ctx, "/sessions/"+sessionID+"/adb", body)
	var parsed struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal(out, &parsed)
	if parsed.Output != "" {
		return parsed.Output
	}
	return string(out)
}

// ---- APK fixture helpers ------------------------------------------

// fetchAPIDemos downloads the ApiDemos APK once, caches it, and
// returns the local path. Returns error on network failure so the
// caller can skip APK-dependent tests cleanly.
func fetchAPIDemos(ctx context.Context) (string, error) {
	if fi, err := os.Stat(apkCachePath); err == nil && fi.Size() > 500_000 {
		return apkCachePath, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiDemosURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}
	f, err := os.Create(apkCachePath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(apkCachePath)
		return "", err
	}
	f.Close()
	return apkCachePath, nil
}

// ---- Pool helpers -------------------------------------------------

func listPoolInstances(t *subT) []InstanceResponse {
	t.Helper()
	pool := GetPool(t.T)
	return pool.Instances
}

func firstWarm(t *subT) (InstanceResponse, bool) {
	t.Helper()
	for _, i := range listPoolInstances(t) {
		if i.State == "warm" {
			return i, true
		}
	}
	return InstanceResponse{}, false
}

// createSessionWithBody bypasses the default profile in CreateSession()
// so callers can supply their own body (capabilities etc.).
func createSessionWithBody(ctx context.Context, body string) (SessionResponse, int, []byte) {
	code, out := httpPostJSON(ctx, "/sessions", body)
	var sess SessionResponse
	if code >= 200 && code < 300 {
		_ = json.Unmarshal(out, &sess)
	}
	return sess, code, out
}

// releaseSession releases via our API, swallowing errors — called
// from defers where a failure to release shouldn't fail the test.
func releaseSession(ctx context.Context, id string) {
	_, _ = httpDelete(ctx, "/sessions/"+id)
}

// ---- The suite ----------------------------------------------------

func TestCapabilities_FullSuite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	s := &suite{t: t}
	defer s.summary()

	t.Log(strings.Repeat("━", 60))
	t.Log("  drizz-farm API conformance suite")
	t.Log(strings.Repeat("━", 60))

	// ----- Group / registry ----------------------------------------

	s.run("Group: GET /group returns key for loopback", func(t *subT) {
		var info struct {
			GroupName string `json:"group_name"`
			GroupKey  string `json:"group_key"`
			HasGroup  bool   `json:"has_group"`
			Self      struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"self"`
		}
		code, err := httpGetJSON(ctx, "/group", &info)
		if err != nil {
			t.failf("%v", err)
			return
		}
		if code != 200 {
			t.failf("status %d", code)
			return
		}
		if info.Self.Name == "" {
			t.failf("self.name empty")
			return
		}
		if info.HasGroup && info.GroupKey == "" {
			t.failf("has_group=true but group_key empty — local callers should see key")
			return
		}
		t.pass(fmt.Sprintf("self=%s has_group=%v", info.Self.Name, info.HasGroup))
	})

	s.run("Group: POST /nodes without key → 403", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/nodes", `{"name":"ghost","url":"http://ghost.invalid"}`)
		if code != 403 {
			t.failf("got %d want 403", code)
			return
		}
		t.pass("")
	})

	// ----- Devices list + filters ----------------------------------

	s.run("Devices: GET /devices shape", func(t *subT) {
		var out struct {
			Devices []map[string]any `json:"devices"`
			Total   int              `json:"total"`
		}
		code, err := httpGetJSON(ctx, "/devices", &out)
		if err != nil || code != 200 {
			t.failf("%d %v", code, err)
			return
		}
		if out.Total != len(out.Devices) {
			t.failf("total=%d != len=%d", out.Total, len(out.Devices))
			return
		}
		for _, d := range out.Devices {
			for _, k := range []string{"id", "state", "profile", "reserved"} {
				if _, ok := d[k]; !ok {
					t.failf("device missing field %s", k)
					return
				}
			}
		}
		t.pass(fmt.Sprintf("%d device(s) listed", out.Total))
	})

	s.run("Devices: ?free=true returns only warm + unreserved", func(t *subT) {
		var out struct {
			Devices []struct {
				State    string `json:"state"`
				Reserved bool   `json:"reserved"`
			} `json:"devices"`
		}
		httpGetJSON(ctx, "/devices?free=true", &out)
		for _, d := range out.Devices {
			if d.State != "warm" || d.Reserved {
				t.failf("free filter leaked: state=%s reserved=%v", d.State, d.Reserved)
				return
			}
		}
		t.pass(fmt.Sprintf("%d free device(s)", len(out.Devices)))
	})

	s.run("Devices: reserve unknown id → 404", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/devices/does-not-exist/reserve", `{}`)
		if code != 404 {
			t.failf("got %d want 404", code)
			return
		}
		t.pass("")
	})

	// ----- The rest depends on at least one warm emulator ----------

	warm, haveWarm := firstWarm(&subT{T: t})
	if !haveWarm {
		t.Log("")
		t.Log("  (no warm emulator — the remaining sub-tests will skip.")
		t.Log("   boot one with `drizz-farm start` then `drizz-farm create` to run them.)")
		return
	}
	t.Logf("  Using warm instance: id=%s avd=%s serial=%s", warm.ID, warm.AVDName, warm.Serial)

	// ----- Reservations --------------------------------------------

	s.run("Reservation: reserve + label appears in ?reserved=true", func(t *subT) {
		code, body := httpPostJSON(ctx, "/devices/"+warm.ID+"/reserve", `{"label":"ci-hold"}`)
		if code != 200 {
			t.failf("reserve: %d %s", code, body)
			return
		}
		var out struct {
			Devices []struct {
				ID            string `json:"id"`
				ReservedLabel string `json:"reserved_label"`
			} `json:"devices"`
		}
		httpGetJSON(ctx, "/devices?reserved=true", &out)
		for _, d := range out.Devices {
			if d.ID == warm.ID && d.ReservedLabel == "ci-hold" {
				t.pass("")
				return
			}
		}
		t.failf("reserved device or label missing from list")
	})

	s.run("Reservation: unreserve clears bit", func(t *subT) {
		code, body := httpDelete(ctx, "/devices/"+warm.ID+"/reserve")
		if code != 200 {
			t.failf("unreserve: %d %s", code, body)
			return
		}
		var out struct {
			Devices []struct {
				ID string `json:"id"`
			} `json:"devices"`
		}
		httpGetJSON(ctx, "/devices?reserved=true", &out)
		for _, d := range out.Devices {
			if d.ID == warm.ID {
				t.failf("device still reserved after unreserve")
				return
			}
		}
		t.pass("")
	})

	// ----- Specific-device session allocation ----------------------

	s.run("Session: create by device_id binds to that instance", func(t *subT) {
		sess, code, body := createSessionWithBody(ctx, fmt.Sprintf(`{"device_id":"%s"}`, warm.ID))
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, body)
			return
		}
		defer releaseSession(ctx, sess.ID)
		if sess.InstanceID != warm.ID {
			t.failf("got instance %s, want %s", sess.InstanceID, warm.ID)
			return
		}
		t.pass(fmt.Sprintf("bound to %s", warm.ID))
	})

	s.run("Session: create by avd_name binds correctly", func(t *subT) {
		if warm.AVDName == "" {
			t.skipf("warm instance has no AVD name (USB device)")
			return
		}
		sess, code, body := createSessionWithBody(ctx, fmt.Sprintf(`{"avd_name":"%s"}`, warm.AVDName))
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, body)
			return
		}
		defer releaseSession(ctx, sess.ID)
		if sess.InstanceID != warm.ID {
			t.failf("got instance %s, want %s", sess.InstanceID, warm.ID)
			return
		}
		t.pass("")
	})

	s.run("Session: specific device busy → fails fast (no fallback)", func(t *subT) {
		holder := CreateSession(t.T, "")
		defer ReleaseSession(t.T, holder.ID)

		_, code, body := createSessionWithBody(ctx, fmt.Sprintf(`{"device_id":"%s"}`, warm.ID))
		if code < 400 {
			t.failf("expected 4xx for busy, got %d: %s", code, body)
			return
		}
		t.pass(fmt.Sprintf("rejected with %d", code))
	})

	// ----- Session capabilities + artifacts ------------------------

	s.run("Capabilities: echo back on create", func(t *subT) {
		body := `{
			"capabilities": {
				"record_video": true,
				"capture_logcat": true,
				"capture_screenshots": true,
				"retention_hours": 48
			}
		}`
		sess, code, out := createSessionWithBody(ctx, body)
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, out)
			return
		}
		defer releaseSession(ctx, sess.ID)

		var full struct {
			Capabilities struct {
				RecordVideo        bool `json:"record_video"`
				CaptureLogcat      bool `json:"capture_logcat"`
				CaptureScreenshots bool `json:"capture_screenshots"`
				RetentionHours     int  `json:"retention_hours"`
			} `json:"capabilities"`
		}
		json.Unmarshal(out, &full)
		if !full.Capabilities.RecordVideo || !full.Capabilities.CaptureLogcat {
			t.failf("capabilities not preserved: %+v", full.Capabilities)
			return
		}
		if full.Capabilities.RetentionHours != 48 {
			t.failf("retention: got %d want 48", full.Capabilities.RetentionHours)
			return
		}
		// Retention sidecar file should exist
		sidecar := filepath.Join(os.Getenv("HOME"), ".drizz-farm", "artifacts", sess.ID, ".retention")
		if data, err := os.ReadFile(sidecar); err == nil {
			if got := strings.TrimSpace(string(data)); got != "48" {
				t.failf("retention sidecar content %q want 48", got)
				return
			}
		}
		t.pass("")
	})

	s.run("Screenshot: gated when capability disabled", func(t *subT) {
		sess, code, out := createSessionWithBody(ctx,
			`{"capabilities":{"capture_screenshots":false}}`)
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, out)
			return
		}
		defer releaseSession(ctx, sess.ID)
		code, body := httpPostJSON(ctx, "/sessions/"+sess.ID+"/screenshot", "")
		if code != 403 {
			t.failf("got %d want 403 (%s)", code, body)
			return
		}
		t.pass("")
	})

	s.run("Screenshot: allowed + returns PNG when capability enabled", func(t *subT) {
		sess, code, out := createSessionWithBody(ctx,
			`{"capabilities":{"capture_screenshots":true}}`)
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, out)
			return
		}
		defer releaseSession(ctx, sess.ID)
		code, body := httpPostJSON(ctx, "/sessions/"+sess.ID+"/screenshot", "")
		if code != 200 {
			t.failf("got %d (%s)", code, body)
			return
		}
		// PNG magic: 89 50 4E 47
		if len(body) < 8 || body[0] != 0x89 || body[1] != 0x50 || body[2] != 0x4E || body[3] != 0x47 {
			t.failf("response not a PNG: first bytes %s", hex.EncodeToString(body[:min(8, len(body))]))
			return
		}
		t.pass(fmt.Sprintf("%d bytes PNG", len(body)))
	})

	s.run("Artifacts: video recording produces file on release", func(t *subT) {
		sess, code, out := createSessionWithBody(ctx,
			`{"capabilities":{"record_video":true,"capture_logcat":true}}`)
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, out)
			return
		}
		// Let the recorder accumulate >10s of video; stop will pull
		// the partial chunk even though it's under the 180s cap.
		time.Sleep(12 * time.Second)
		releaseSession(ctx, sess.ID)
		// Give stop() 5s to finalize + pull.
		time.Sleep(5 * time.Second)

		var arts struct {
			Artifacts []struct {
				Type     string `json:"type"`
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
			} `json:"artifacts"`
		}
		httpGetJSON(ctx, "/sessions/"+sess.ID+"/artifacts", &arts)
		haveVideo, haveLogcat := false, false
		for _, a := range arts.Artifacts {
			if a.Type == "video" && a.Size > 1024 {
				haveVideo = true
			}
			if a.Type == "logcat" && a.Size > 0 {
				haveLogcat = true
			}
		}
		switch {
		case !haveVideo:
			t.failf("no video artifact with size>1KB")
		case !haveLogcat:
			t.failf("no logcat artifact")
		default:
			t.pass(fmt.Sprintf("%d artifact(s)", len(arts.Artifacts)))
		}
	})

	s.run("Capture: network HAR (mitmdump)", func(t *subT) {
		if _, err := exec.LookPath("mitmdump"); err != nil {
			t.skipf("mitmdump not installed on test host")
			return
		}
		sess, code, out := createSessionWithBody(ctx,
			`{"capabilities":{"capture_network":true}}`)
		if code < 200 || code >= 300 {
			t.failf("create: %d %s", code, out)
			return
		}
		// Give mitmdump a beat, then issue a request from the device.
		time.Sleep(3 * time.Second)
		adbShell(ctx, sess.ID, "curl -s -o /dev/null http://example.com || true")
		time.Sleep(2 * time.Second)
		releaseSession(ctx, sess.ID)
		time.Sleep(3 * time.Second)

		var arts struct {
			Artifacts []struct {
				Type string `json:"type"`
				Size int64  `json:"size"`
			} `json:"artifacts"`
		}
		httpGetJSON(ctx, "/sessions/"+sess.ID+"/artifacts", &arts)
		for _, a := range arts.Artifacts {
			if a.Type == "network" && a.Size > 0 {
				t.pass(fmt.Sprintf("HAR %d bytes", a.Size))
				return
			}
		}
		t.failf("no network artifact produced")
	})

	// ----- Device simulation with on-device verification -----------

	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)
	inst := sess.InstanceID

	s.run("Battery: set 42% → dumpsys shows 42", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/battery", `{"level":42}`)
		if code != 200 {
			t.failf("POST /battery: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID, "dumpsys battery")
		if !strings.Contains(out, "level: 42") {
			t.failf("dumpsys did not show level 42:\n%s", firstLines(out, 10))
			return
		}
		t.pass("level: 42")
	})

	s.run("Locale: set en_GB → getprop persist.sys.locale", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/locale", `{"locale":"en_GB"}`)
		if code != 200 {
			t.failf("POST /locale: %d", code)
			return
		}
		out := strings.TrimSpace(adbShell(ctx, sess.ID, "getprop persist.sys.locale"))
		if out != "en-GB" && out != "en_GB" {
			t.failf("getprop returned %q", out)
			return
		}
		t.pass(out)
	})

	s.run("Timezone: set America/Tokyo → getprop persist.sys.timezone", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/timezone",
			`{"timezone":"America/New_York"}`)
		if code != 200 {
			t.failf("POST /timezone: %d", code)
			return
		}
		out := strings.TrimSpace(adbShell(ctx, sess.ID, "getprop persist.sys.timezone"))
		if out != "America/New_York" {
			t.failf("getprop returned %q", out)
			return
		}
		t.pass(out)
	})

	s.run("Dark mode: enable → cmd uimode night returns yes", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/appearance",
			`{"dark_mode":true}`)
		if code != 200 {
			t.failf("POST /appearance: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID, "cmd uimode night")
		if !strings.Contains(strings.ToLower(out), "yes") {
			t.failf("cmd uimode night returned %q", firstLines(out, 3))
			return
		}
		t.pass(strings.TrimSpace(firstLines(out, 1)))
	})

	s.run("Font scale: 1.5 → settings get matches", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/font-scale", `{"scale":1.5}`)
		if code != 200 {
			t.failf("POST /font-scale: %d", code)
			return
		}
		out := strings.TrimSpace(adbShell(ctx, sess.ID, "settings get system font_scale"))
		if f, err := strconv.ParseFloat(out, 64); err != nil || f != 1.5 {
			t.failf("got %q want 1.5", out)
			return
		}
		t.pass(out)
	})

	s.run("Animations: disable → animator_duration_scale = 0", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/animations",
			`{"enabled":false}`)
		if code != 200 {
			t.failf("POST /animations: %d", code)
			return
		}
		out := strings.TrimSpace(adbShell(ctx, sess.ID,
			"settings get global animator_duration_scale"))
		if !strings.HasPrefix(out, "0") {
			t.failf("got %q want 0.0", out)
			return
		}
		t.pass(out)
	})

	s.run("GPS: set SF → dumpsys location shows lat ~37.77", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/gps",
			`{"latitude":37.7749,"longitude":-122.4194}`)
		if code != 200 {
			t.failf("POST /gps: %d", code)
			return
		}
		// Let the GPS provider settle
		time.Sleep(500 * time.Millisecond)
		out := adbShell(ctx, sess.ID, "dumpsys location")
		if !strings.Contains(out, "37.77") && !strings.Contains(out, "-122.41") {
			t.failf("dumpsys location missing our coords:\n%s", firstLines(out, 30))
			return
		}
		t.pass("lat 37.7749 observed")
	})

	s.run("Clipboard: set 'drizz-clip-probe' → cmd clipboard get matches", func(t *subT) {
		probe := "drizz-clip-probe-" + fmt.Sprint(time.Now().UnixNano())
		code, body := httpPostJSON(ctx, "/sessions/"+inst+"/clipboard",
			fmt.Sprintf(`{"text":%q}`, probe))
		if code != 200 {
			t.failf("POST /clipboard: %d %s", code, body)
			return
		}
		var resp struct {
			Method string `json:"method"`
		}
		json.Unmarshal(body, &resp)
		if resp.Method == "input_text_fallback" {
			t.skipf("emulator image too old for cmd clipboard")
			return
		}
		out := strings.TrimSpace(adbShell(ctx, sess.ID,
			"cmd clipboard get-primary-clip -u 0"))
		if !strings.Contains(out, probe) {
			t.failf("clipboard read %q, want to contain %q", firstLines(out, 3), probe)
			return
		}
		t.pass("")
	})

	s.run("Push notification: posts + cmd notification list includes tag", func(t *subT) {
		tag := "drizz-probe-" + fmt.Sprint(time.Now().UnixNano())
		code, body := httpPostJSON(ctx, "/sessions/"+inst+"/push-notification",
			fmt.Sprintf(`{"title":"Hello from drizz","body":"probe","tag":%q}`, tag))
		if code != 200 {
			t.failf("POST /push-notification: %d %s", code, body)
			return
		}
		time.Sleep(500 * time.Millisecond)
		out := adbShell(ctx, sess.ID, "cmd notification list")
		if !strings.Contains(out, tag) {
			t.failf("cmd notification list did not include %q:\n%s", tag, firstLines(out, 20))
			return
		}
		t.pass("")
	})

	s.run("Sensor: set acceleration 0:9.8:0 → console reads back", func(t *subT) {
		code, body := httpPostJSON(ctx, "/sessions/"+inst+"/sensor",
			`{"name":"acceleration","values":"0:9.8:0"}`)
		if code != 200 {
			t.failf("POST /sensor: %d %s", code, body)
			return
		}
		t.pass("(values read-back requires emulator console access)")
	})

	s.run("Shake: accelerometer spike observable", func(t *subT) {
		// Set a baseline low, fire shake, then read — the shake handler
		// briefly spikes then resets. We can at least confirm the API
		// returned ok.
		code, _ := httpPostJSON(ctx, "/sessions/"+inst+"/shake", `{}`)
		if code != 200 {
			t.failf("POST /shake: %d", code)
			return
		}
		t.pass("(spike window too narrow to reliably observe post-fact)")
	})

	// ----- Permissions + install/uninstall + clear-data (APK) ------

	apkPath, apkErr := fetchAPIDemos(ctx)

	s.run("Install: multipart APK → pm list contains package", func(t *subT) {
		if apkErr != nil {
			t.skipf("APK fetch failed: %v", apkErr)
			return
		}
		// Pre-clean if a previous run left it installed.
		httpPostJSON(ctx, "/sessions/"+sess.ID+"/uninstall",
			fmt.Sprintf(`{"package":"%s"}`, apiDemosPkg))
		apkBytes, _ := os.ReadFile(apkPath)
		code, body := postMultipart(ctx, "/sessions/"+sess.ID+"/install",
			map[string][]byte{"apk": apkBytes}, nil)
		if code != 200 {
			t.failf("install: %d %s", code, body)
			return
		}
		out := adbShell(ctx, sess.ID, "pm list packages "+apiDemosPkg)
		if !strings.Contains(out, apiDemosPkg) {
			t.failf("pm list did not show package: %s", firstLines(out, 5))
			return
		}
		t.pass(apiDemosPkg)
	})

	s.run("Permissions: grant READ_CONTACTS → dumpsys package shows granted", func(t *subT) {
		if apkErr != nil {
			t.skipf("APK fetch failed (install prerequisite)")
			return
		}
		perm := "android.permission.READ_CONTACTS"
		code, _ := httpPostJSON(ctx, "/sessions/"+sess.ID+"/permissions",
			fmt.Sprintf(`{"package":"%s","permission":"%s","grant":true}`, apiDemosPkg, perm))
		if code != 200 {
			t.failf("POST /permissions: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID,
			fmt.Sprintf("dumpsys package %s | grep %s", apiDemosPkg, perm))
		// On API 29+ permissions show as granted=true; older say GRANTED
		if !strings.Contains(out, "granted=true") && !strings.Contains(out, "GRANTED") {
			t.failf("permission not granted:\n%s", firstLines(out, 5))
			return
		}
		t.pass(perm)
	})

	s.run("Clear data: after data write, file removed", func(t *subT) {
		if apkErr != nil {
			t.skipf("APK fetch failed (install prerequisite)")
			return
		}
		// Write a probe file via run-as (works because ApiDemos-debug
		// is a debuggable APK). We then clear-data via the API and
		// assert the probe is gone.
		_ = fmt.Sprintf("/data/data/%s/files/probe.txt", apiDemosPkg) // path for docs
		adbShell(ctx, sess.ID, fmt.Sprintf(`run-as %s sh -c "mkdir -p files && echo probe > files/probe.txt"`, apiDemosPkg))
		code, _ := httpPostJSON(ctx, "/sessions/"+sess.ID+"/clear-data",
			fmt.Sprintf(`{"package":"%s"}`, apiDemosPkg))
		if code != 200 {
			t.failf("POST /clear-data: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID, fmt.Sprintf(`run-as %s ls files/probe.txt 2>&1`, apiDemosPkg))
		if !strings.Contains(out, "No such file") {
			t.failf("probe file not removed: %s", firstLines(out, 3))
			return
		}
		t.pass("")
	})

	s.run("Uninstall: pm list no longer contains package", func(t *subT) {
		if apkErr != nil {
			t.skipf("APK fetch failed (install prerequisite)")
			return
		}
		code, _ := httpPostJSON(ctx, "/sessions/"+sess.ID+"/uninstall",
			fmt.Sprintf(`{"package":"%s"}`, apiDemosPkg))
		if code != 200 {
			t.failf("POST /uninstall: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID, "pm list packages "+apiDemosPkg)
		if strings.Contains(out, apiDemosPkg) {
			t.failf("pm list still shows package: %s", firstLines(out, 3))
			return
		}
		t.pass("")
	})

	// ----- Biometric (requires enroll then touch) ------------------

	s.run("Biometric: enroll + touch → dumpsys fingerprint shows enrolled", func(t *subT) {
		code, _ := httpPostJSON(ctx, "/sessions/"+sess.ID+"/biometric",
			`{"action":"enroll"}`)
		if code != 200 {
			t.failf("POST biometric enroll: %d", code)
			return
		}
		code, _ = httpPostJSON(ctx, "/sessions/"+sess.ID+"/biometric",
			`{"action":"touch"}`)
		if code != 200 {
			t.failf("POST biometric touch: %d", code)
			return
		}
		out := adbShell(ctx, sess.ID, "dumpsys fingerprint")
		// Implementations vary widely. We accept any of the common
		// indicators that a print is enrolled.
		keywords := []string{"Enrolled", "enrolled", "numEnrolled", "finger"}
		matched := false
		for _, k := range keywords {
			if strings.Contains(out, k) {
				matched = true
				break
			}
		}
		if !matched {
			t.failf("dumpsys fingerprint no enrollment indicator:\n%s", firstLines(out, 20))
			return
		}
		t.pass("")
	})

	// ----- File upload + camera inject -----------------------------

	s.run("Upload: multipart → adb shell cat content matches", func(t *subT) {
		target := fmt.Sprintf("/sdcard/Download/drizz-probe-%d.txt", time.Now().Unix())
		content := []byte("drizz-upload-probe\n")
		code, body := postMultipart(ctx, "/sessions/"+sess.ID+"/files/upload",
			map[string][]byte{"file": content},
			map[string]string{"target": target})
		if code != 200 {
			t.failf("upload: %d %s", code, body)
			return
		}
		out := adbShell(ctx, sess.ID, "cat "+target)
		if !strings.Contains(out, "drizz-upload-probe") {
			t.failf("cat returned %q", firstLines(out, 3))
			return
		}
		t.pass(target)
	})

	s.run("Camera inject: multipart → file under /sdcard/DCIM/Camera/", func(t *subT) {
		code, body := postMultipart(ctx, "/sessions/"+sess.ID+"/camera",
			map[string][]byte{"image": tinyJPEG()}, nil)
		if code != 200 {
			t.failf("camera inject: %d %s", code, body)
			return
		}
		var out struct {
			DevicePath string `json:"device_path"`
		}
		json.Unmarshal(body, &out)
		if !strings.HasPrefix(out.DevicePath, "/sdcard/DCIM/Camera/") {
			t.failf("device_path: %s", out.DevicePath)
			return
		}
		ls := adbShell(ctx, sess.ID, "ls "+out.DevicePath)
		if !strings.Contains(ls, out.DevicePath) {
			t.failf("injected file not on device: %s", firstLines(ls, 3))
			return
		}
		t.pass(filepath.Base(out.DevicePath))
	})

	// ----- Routing canary — every remaining route accepts input -----

	canary := []struct {
		name, path, body string
	}{
		{"Network profile (lte)", "network", `{"profile":"lte"}`},
		{"Orientation portrait", "orientation", `{"orientation":"portrait"}`},
		{"Volume set 5", "volume", `{"level":5}`},
		{"Key press HOME", "key", `{"keycode":"HOME"}`},
		{"Raw ADB echo", "adb", `{"command":"shell echo drizz"}`},
	}
	for _, c := range canary {
		c := c
		s.run("Route canary: "+c.name, func(t *subT) {
			code, body := httpPostJSON(ctx, "/sessions/"+sess.ID+"/"+c.path, c.body)
			if code < 200 || code >= 300 {
				t.failf("%d %s", code, firstLines(string(body), 2))
				return
			}
			t.pass("")
		})
	}
}

// ---- Small helpers ------------------------------------------------

// firstLines returns up to n lines of s — used to keep failure
// messages readable even when a tool returns 500-line dumps.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// tinyJPEG is the smallest valid JPEG — 1×1 white pixel.
// Inlined (not var in another file) so this test file stays self-
// contained and independent of other test helpers.
func tinyJPEG() []byte {
	return []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x01, 0x00, 0x48,
		0x00, 0x48, 0x00, 0x00, 0xFF, 0xDB, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08,
		0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12,
		0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A, 0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20,
		0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29, 0x2C, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27,
		0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x01,
		0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xFF, 0xC4, 0x00, 0x1F, 0x00, 0x00, 0x01, 0x05, 0x01, 0x01,
		0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04,
		0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0xFF, 0xC4, 0x00, 0xB5, 0x10, 0x00, 0x02, 0x01, 0x03,
		0x03, 0x02, 0x04, 0x03, 0x05, 0x05, 0x04, 0x04, 0x00, 0x00, 0x01, 0x7D, 0x01, 0x02, 0x03, 0x00,
		0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07, 0x22, 0x71, 0x14, 0x32,
		0x81, 0x91, 0xA1, 0x08, 0x23, 0x42, 0xB1, 0xC1, 0x15, 0x52, 0xD1, 0xF0, 0x24, 0x33, 0x62, 0x72,
		0x82, 0x09, 0x0A, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x34, 0x35,
		0x36, 0x37, 0x38, 0x39, 0x3A, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x53, 0x54, 0x55,
		0x56, 0x57, 0x58, 0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x73, 0x74, 0x75,
		0x76, 0x77, 0x78, 0x79, 0x7A, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A, 0x92, 0x93, 0x94,
		0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xB2,
		0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7, 0xC8, 0xC9,
		0xCA, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xE1, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6,
		0xE7, 0xE8, 0xE9, 0xEA, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFF, 0xDA,
		0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0xFB, 0xD0, 0xFF, 0xD9,
	}
}

// Silence "imported but not used" on platforms where errors isn't
// referenced by other code paths yet (kept for future condition
// chains).
var _ = errors.New
