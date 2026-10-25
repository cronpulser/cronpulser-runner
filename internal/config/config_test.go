package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSupportsIntegerTimeout(t *testing.T) {
	directory := t.TempDir()
	command := filepath.Join(directory, "job.sh")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "runner.yaml")
	content := "server: http://localhost:8080\nrunnerId: runner-1\nrunnerSecret: secret\njobs:\n  hello:\n    command: " + command + "\n    timeout: 30\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs["hello"].Timeout.Duration != 30*time.Second {
		t.Fatalf("unexpected timeout: %v", cfg.Jobs["hello"].Timeout.Duration)
	}
}
