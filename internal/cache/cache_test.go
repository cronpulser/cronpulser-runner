package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/protocol"
)

func TestCachePersistsCompletedResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions.json")
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	exit := 0
	message := protocol.Message{Type: protocol.TypeFinished, ExecutionID: "exec-1", ExitCode: &exit}
	if err := cache.Put("exec-1", protocol.TypeFinished, message); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reloaded.Get("exec-1")
	if !ok || entry.Message.ExitCode == nil || *entry.Message.ExitCode != 0 {
		t.Fatalf("completed result was not persisted: %#v", entry)
	}
}

func TestCacheMarksInterruptedExecutionFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions.json")
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	started := protocol.Message{Type: protocol.TypeStarted, ExecutionID: "exec-2", Timestamp: time.Now()}
	if err := cache.Put("exec-2", protocol.TypeStarted, started); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reloaded.Get("exec-2")
	if !ok || entry.State != protocol.TypeFailed || entry.Message.Error == nil {
		t.Fatalf("interrupted execution was not made terminal: %#v", entry)
	}
	if !entry.Pending {
		t.Fatal("recent interrupted execution should be reported to the server")
	}
}

func TestCacheTracksResultDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions.json")
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	accepted := protocol.Message{Type: protocol.TypeAccepted, ExecutionID: "exec-3"}
	if err := cache.Put("exec-3", protocol.TypeAccepted, accepted); err != nil {
		t.Fatal(err)
	}
	if cache.IsPending("exec-3") || len(cache.Pending()) != 0 {
		t.Fatal("non-final states must not be queued for delivery")
	}
	finished := protocol.Message{Type: protocol.TypeFinished, ExecutionID: "exec-3"}
	if err := cache.Put("exec-3", protocol.TypeFinished, finished); err != nil {
		t.Fatal(err)
	}
	if pending := cache.Pending(); len(pending) != 1 || pending[0].ExecutionID != "exec-3" {
		t.Fatalf("final result was not queued: %#v", pending)
	}
	if err := cache.MarkDelivered("exec-3"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsPending("exec-3") || len(reloaded.Pending()) != 0 {
		t.Fatal("delivered result must stay delivered after a restart")
	}
}

func TestCacheDoesNotReportOldInterruptedExecutions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions.json")
	old := `{"exec-4":{"state":"job.started","message":{"type":"job.started","executionId":"exec-4"},"updatedAt":"2026-01-01T00:00:00Z"}}`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cache.Get("exec-4")
	if entry.State != protocol.TypeFailed || entry.Pending {
		t.Fatalf("old interrupted execution should be closed locally without a report: %#v", entry)
	}
}
