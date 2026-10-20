package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

type Options struct {
	Server, Token, ConfigPath, BinaryPath, UnitPath, StateDir, Version string
	StartService                                                       bool
}

type RotateOptions struct {
	Token, ConfigPath, Version string
	RestartService             bool
}

type registrationResponse struct {
	RunnerID     string `json:"runnerId"`
	RunnerSecret string `json:"runnerSecret"`
}

func register(server, token, expectedRunnerID, version string) (*registrationResponse, error) {
	hostname, _ := os.Hostname()
	payload, _ := json.Marshal(map[string]string{"installToken": token, "runnerId": expectedRunnerID, "hostname": hostname, "version": version, "os": runtime.GOOS + "/" + runtime.GOARCH})
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(server, "/")+"/api/runners/register", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("register runner: server returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result registrationResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode registration response: %w", err)
	}
	if result.RunnerID == "" || result.RunnerSecret == "" {
		return nil, errors.New("registration response omitted credentials")
	}
	return &result, nil
}
