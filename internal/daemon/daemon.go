package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PIDFile manages the daemon PID file.
type PIDFile struct {
	Path string
}

// NewPIDFile creates a PID file manager for the given data directory.
func NewPIDFile(dataDir string) *PIDFile {
	return &PIDFile{
		Path: filepath.Join(dataDir, "drizz-farm.pid"),
	}
}

// Write writes the current PID to the file.
func (p *PIDFile) Write() error {
	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	return os.WriteFile(p.Path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// Read returns the PID from the file, or 0 if not found.
func (p *PIDFile) Read() (int, error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}
	return pid, nil
}

// Remove deletes the PID file.
func (p *PIDFile) Remove() error {
	return os.Remove(p.Path)
}

// IsRunning checks if a process with the stored PID is alive.
func (p *PIDFile) IsRunning() bool {
	pid, err := p.Read()
	if err != nil || pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists
	return proc.Signal(syscall.Signal(0)) == nil
}

// Signal sends a signal to the running daemon process.
func (p *PIDFile) Signal(sig syscall.Signal) error {
	pid, err := p.Read()
	if err != nil {
		return fmt.Errorf("signal daemon: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("no daemon running (pid file not found)")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return proc.Signal(sig)
}
