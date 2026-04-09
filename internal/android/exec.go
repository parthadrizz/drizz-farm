package android

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	// Run executes a command and returns its combined output.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	// Start executes a command in the background and returns the Cmd handle.
	Start(ctx context.Context, name string, args ...string) (*exec.Cmd, error)
}

// DefaultRunner executes real shell commands.
type DefaultRunner struct{}

func (r *DefaultRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	logger := log.With().
		Str("cmd", name).
		Strs("args", args).
		Dur("duration", duration).
		Logger()

	if err != nil {
		logger.Debug().
			Str("stderr", strings.TrimSpace(stderr.String())).
			Err(err).
			Msg("command failed")
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	logger.Debug().Msg("command succeeded")
	return stdout.Bytes(), nil
}

func (r *DefaultRunner) Start(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	log.Debug().
		Str("cmd", name).
		Strs("args", args).
		Msg("starting background command")

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return cmd, nil
}
