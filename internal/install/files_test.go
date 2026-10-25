package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreviousBinaryPathKeepsExtension(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/cronpulser-runner":                  "/usr/local/bin/cronpulser-runner.previous",
		`C:\Program Files\CronPulser\cronpulser-runner.exe`: `C:\Program Files\CronPulser\cronpulser-runner.previous.exe`,
	}
	for input, expected := range cases {
		if actual := previousBinaryPath(input); actual != expected {
			t.Fatalf("previousBinaryPath(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestCopyFileReplacesDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "new")
	destination := filepath.Join(directory, "installed")
	if err := os.WriteFile(source, []byte("new binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Fatalf("destination contains %q", data)
	}
	if _, err := os.Stat(destination + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}
