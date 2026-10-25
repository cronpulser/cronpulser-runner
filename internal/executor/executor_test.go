//go:build !windows

package executor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/config"
	"github.com/cronpulser/cronpulser-runner/internal/protocol"
)

func TestRunPassesArgumentsWithoutShellParsing(t *testing.T) {
	message := Run(context.Background(), "execution-1", config.Job{
		Command:   "/bin/sh",
		Arguments: []string{"-c", `printf '%s' "$1"`, "cronpulser", "hello world"},
		Timeout:   config.Duration{Duration: 5 * time.Second},
	})
	if message.Type != protocol.TypeFinished {
		t.Fatalf("expected finished message, got %q: %v", message.Type, message.Error)
	}
	if message.Stdout == nil || *message.Stdout != "hello world" {
		t.Fatalf("unexpected stdout: %#v", message.Stdout)
	}
}

func TestRunReturnsWhenEscapedChildKeepsOutputOpen(t *testing.T) {
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("setsid not available")
	}
	previous := waitDelay
	waitDelay = 500 * time.Millisecond
	defer func() { waitDelay = previous }()

	started := time.Now()
	message := Run(context.Background(), "execution-2", config.Job{
		Command:   "/bin/sh",
		Arguments: []string{"-c", setsid + " sleep 30 & sleep 30"},
		Timeout:   config.Duration{Duration: time.Second},
	})
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("runner blocked for %s after the timeout", elapsed)
	}
	if message.Type != protocol.TypeFailed {
		t.Fatalf("expected failed message, got %q", message.Type)
	}
}
