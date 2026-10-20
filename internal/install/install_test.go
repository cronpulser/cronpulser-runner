package install

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterBindsRotationToExpectedRunner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runners/register" {
			t.Fatalf("unexpected registration path: %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["installToken"] != "rotation-token" {
			t.Fatalf("unexpected token: %q", payload["installToken"])
		}
		if payload["runnerId"] != "runner-1" {
			t.Fatalf("expected bound runner id, got %q", payload["runnerId"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runnerId":"runner-1","runnerSecret":"new-secret"}`))
	}))
	defer server.Close()

	credentials, err := register(server.URL, "rotation-token", "runner-1", "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.RunnerID != "runner-1" || credentials.RunnerSecret != "new-secret" {
		t.Fatalf("unexpected credentials: %#v", credentials)
	}
}
