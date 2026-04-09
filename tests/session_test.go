//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestSession_CreateAndRelease(t *testing.T) {
	sess := CreateSession(t, "")

	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if sess.State != "active" {
		t.Errorf("expected active, got %s", sess.State)
	}
	if sess.Connection.ADBPort == 0 {
		t.Error("expected non-zero ADB port")
	}
	if sess.Connection.Host == "" {
		t.Error("expected non-empty host")
	}
	if sess.Connection.ADBSerial == "" {
		t.Error("expected non-empty ADB serial")
	}

	ReleaseSession(t, sess.ID)

	// Wait for reset
	time.Sleep(3 * time.Second)

	// Verify it's warm now
	pool := GetPool(t)
	found := false
	for _, inst := range pool.Instances {
		if inst.ID == sess.InstanceID && inst.State == "warm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("instance %s not warm after release", sess.InstanceID)
	}
}

func TestSession_ReusesWarmEmulator(t *testing.T) {
	// First session — boots on demand
	s1 := CreateSession(t, "")
	ReleaseSession(t, s1.ID)
	time.Sleep(3 * time.Second)

	// Second session — should reuse warm emulator (fast)
	start := time.Now()
	s2 := CreateSession(t, "")
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("warm allocation took %s (expected <5s)", elapsed)
	}

	ReleaseSession(t, s2.ID)
}

func TestSession_List(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIGet(t, "/sessions")
	var result struct {
		Sessions []SessionResponse `json:"sessions"`
		Active   int               `json:"active"`
	}
	json.Unmarshal(data, &result)

	if result.Active < 1 {
		t.Errorf("expected >= 1 active, got %d", result.Active)
	}

	found := false
	for _, s := range result.Sessions {
		if s.ID == sess.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("session %s not in list", sess.ID)
	}
}

func TestSession_GetByID(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	data := APIGet(t, "/sessions/"+sess.ID)
	var got SessionResponse
	json.Unmarshal(data, &got)

	if got.ID != sess.ID {
		t.Errorf("expected %s, got %s", sess.ID, got.ID)
	}
}

func TestSession_GetNonexistent(t *testing.T) {
	resp, err := http.Get(apiBase + "/sessions/fake-id-12345")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestSession_DoubleRelease(t *testing.T) {
	sess := CreateSession(t, "")
	ReleaseSession(t, sess.ID)
	time.Sleep(1 * time.Second)

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/sessions/%s", apiBase, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		t.Error("expected error on double release")
	}
}
