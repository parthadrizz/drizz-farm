//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const apiBase = "http://127.0.0.1:9401/api/v1"

var daemonCmd *exec.Cmd

func TestMain(m *testing.M) {
	build := exec.Command("go", "build", "-o", "/tmp/drizz-farm-test", ".")
	build.Dir = findProjectRoot()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Printf("BUILD FAILED: %s\n%s\n", err, out)
		os.Exit(1)
	}

	daemonCmd = exec.Command("/tmp/drizz-farm-test", "start")
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr
	if err := daemonCmd.Start(); err != nil {
		fmt.Printf("DAEMON START FAILED: %s\n", err)
		os.Exit(1)
	}

	if !waitForAPI(30 * time.Second) {
		fmt.Println("DAEMON DID NOT START IN TIME")
		daemonCmd.Process.Kill()
		os.Exit(1)
	}

	code := m.Run()

	daemonCmd.Process.Kill()
	daemonCmd.Wait()
	os.Exit(code)
}

// --- Types ---

type PoolResponse struct {
	TotalCapacity int                `json:"total_capacity"`
	Warm          int                `json:"warm"`
	Allocated     int                `json:"allocated"`
	Booting       int                `json:"booting"`
	Error         int                `json:"error"`
	Instances     []InstanceResponse `json:"instances"`
}

type InstanceResponse struct {
	ID      string `json:"id"`
	AVDName string `json:"avd_name"`
	State   string `json:"state"`
	Serial  string `json:"serial"`
	ADBPort int    `json:"adb_port"`
}

type SessionResponse struct {
	ID         string `json:"id"`
	Profile    string `json:"profile"`
	State      string `json:"state"`
	InstanceID string `json:"instance_id"`
	Connection struct {
		Host      string `json:"host"`
		ADBPort   int    `json:"adb_port"`
		ADBSerial string `json:"adb_serial"`
	} `json:"connection"`
}

// --- Helpers ---

func GetPool(t *testing.T) PoolResponse {
	t.Helper()
	data := APIGet(t, "/pool")
	var pool PoolResponse
	if err := json.Unmarshal(data, &pool); err != nil {
		t.Fatalf("unmarshal pool: %v", err)
	}
	return pool
}

func CreateSession(t *testing.T, profile string) SessionResponse {
	t.Helper()
	if profile == "" {
		profile = "api34_ext8_play"
	}
	body := fmt.Sprintf(`{"profile":"%s"}`, profile)
	resp, err := http.Post(apiBase+"/sessions", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("create session failed (%d): %s", resp.StatusCode, data)
	}
	var sess SessionResponse
	json.Unmarshal(data, &sess)
	return sess
}

func ReleaseSession(t *testing.T, id string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/sessions/%s", apiBase, id), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("release session: %v", err)
	}
	resp.Body.Close()
}

func APIGet(t *testing.T, path string) []byte {
	t.Helper()
	resp, err := http.Get(apiBase + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
}

func waitForAPI(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(apiBase + "/node/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func findProjectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
