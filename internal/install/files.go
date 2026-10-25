package install

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// upgradeHealthWindow is how long the service must stay running after an
// upgrade before the new binary is kept.
const upgradeHealthWindow = 6 * time.Second

type UpgradeOptions struct {
	ConfigPath, BinaryPath, Version string
	RestartService                  bool
}

// copyFile replaces destination with source through a temporary file, so a
// failed copy never leaves a partial binary at the destination.
func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	temporary := destination + ".tmp"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func currentExecutable() (string, error) {
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return source, nil
	}
	return resolved, nil
}

// previousBinaryPath keeps the extension so a restored Windows binary stays
// an .exe.
func previousBinaryPath(binaryPath string) string {
	extension := filepath.Ext(binaryPath)
	return strings.TrimSuffix(binaryPath, extension) + ".previous" + extension
}

func installedVersion(binaryPath string) string {
	output, err := exec.Command(binaryPath, "version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}
