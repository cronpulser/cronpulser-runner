//go:build !windows

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/cache"
	"github.com/cronpulser/cronpulser-runner/internal/config"
	"github.com/cronpulser/cronpulser-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

// fakeServer accepts runner connections and hands each one to the test.
type fakeServer struct {
	server      *httptest.Server
	connections chan *websocket.Conn
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{connections: make(chan *websocket.Conn, 4)}
	upgrader := websocket.Upgrader{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		fake.connections <- connection
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeServer) next(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case connection := <-f.connections:
		return connection
	case <-time.After(10 * time.Second):
		t.Fatal("runner did not connect")
		return nil
	}
}

// waitFor reads messages until one of the wanted type arrives, or reports
// whether none arrived within the timeout.
func waitFor(connection *websocket.Conn, messageType string, timeout time.Duration) (protocol.Message, bool) {
	deadline := time.Now().Add(timeout)
	for {
		_ = connection.SetReadDeadline(deadline)
		var message protocol.Message
		if err := connection.ReadJSON(&message); err != nil {
			return protocol.Message{}, false
		}
		if message.Type == messageType {
			return message, true
		}
	}
}

func TestRunningJobSurvivesDisconnectAndResultIsDeliveredOnce(t *testing.T) {
	previous := reconnectDelays
	reconnectDelays = []time.Duration{100 * time.Millisecond}
	defer func() { reconnectDelays = previous }()

	fake := newFakeServer(t)
	executionCache, err := cache.Load(filepath.Join(t.TempDir(), "executions.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Server: fake.server.URL, RunnerID: "runner-1", RunnerSecret: "secret", MaxConcurrentJobs: 1,
		Jobs: map[string]config.Job{"slow": {
			Command:   "/bin/sh",
			Arguments: []string{"-c", "sleep 1; echo done"},
			Timeout:   config.Duration{Duration: 10 * time.Second},
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = New(cfg, executionCache, "test").Run(ctx) }()

	first := fake.next(t)
	if err := first.WriteJSON(protocol.Message{Type: protocol.TypeExecute, ExecutionID: "exec-1", JobKey: "slow"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := waitFor(first, protocol.TypeStarted, 5*time.Second); !ok {
		t.Fatal("job did not start")
	}
	// Drop the connection while the job is still running.
	first.Close()

	second := fake.next(t)
	finished, ok := waitFor(second, protocol.TypeFinished, 5*time.Second)
	if !ok {
		t.Fatal("result was not delivered after reconnecting")
	}
	if finished.ExecutionID != "exec-1" || finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("job did not complete successfully: %#v", finished)
	}
	if finished.Stdout == nil || *finished.Stdout != "done\n" {
		t.Fatalf("unexpected stdout: %#v", finished.Stdout)
	}
	second.Close()

	third := fake.next(t)
	if message, ok := waitFor(third, protocol.TypeFinished, time.Second); ok {
		t.Fatalf("delivered result was sent again: %#v", message)
	}
	third.Close()
}
