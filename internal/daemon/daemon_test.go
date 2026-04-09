package daemon

import (
	"os"
	"testing"
)

func TestPIDFileWriteRead(t *testing.T) {
	dir := t.TempDir()
	pf := NewPIDFile(dir)

	if err := pf.Write(); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	pid, err := pf.Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestPIDFileRemove(t *testing.T) {
	dir := t.TempDir()
	pf := NewPIDFile(dir)

	pf.Write()
	pf.Remove()

	pid, err := pf.Read()
	if err != nil {
		t.Fatalf("Read() after remove error: %v", err)
	}
	if pid != 0 {
		t.Errorf("expected 0 after remove, got %d", pid)
	}
}

func TestPIDFileIsRunning(t *testing.T) {
	dir := t.TempDir()
	pf := NewPIDFile(dir)

	// No PID file — not running
	if pf.IsRunning() {
		t.Error("expected not running with no PID file")
	}

	// Write current PID — should be running
	pf.Write()
	if !pf.IsRunning() {
		t.Error("expected running after writing current PID")
	}

	// Remove — not running
	pf.Remove()
	if pf.IsRunning() {
		t.Error("expected not running after remove")
	}
}

func TestPIDFileReadNonExistent(t *testing.T) {
	dir := t.TempDir()
	pf := NewPIDFile(dir)

	pid, err := pf.Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if pid != 0 {
		t.Errorf("expected 0 for nonexistent, got %d", pid)
	}
}

func TestPIDFilePath(t *testing.T) {
	pf := NewPIDFile("/tmp/test-drizz")
	expected := "/tmp/test-drizz/drizz-farm.pid"
	if pf.Path != expected {
		t.Errorf("expected %s, got %s", expected, pf.Path)
	}
}
