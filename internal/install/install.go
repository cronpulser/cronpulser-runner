//go:build !windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cronpulser/cronpulser-runner/internal/config"
)

const (
	DefaultBinaryPath = "/usr/local/bin/cronpulser-runner"
	DefaultUnitPath   = "/etc/systemd/system/cronpulser-runner.service"
	DefaultStateDir   = "/var/lib/cronpulser"
)

func command(name string, arguments ...string) error {
	cmd := exec.Command(name, arguments...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func ensureUser() error {
	if _, err := user.Lookup("cronpulser"); err == nil {
		return nil
	}
	return command("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "cronpulser")
}

func copyBinary(destination string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	source, _ = filepath.EvalSymlinks(source)
	if source == destination {
		return nil
	}
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
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func chown(path string) error {
	account, err := user.Lookup("cronpulser")
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	return os.Chown(path, uid, gid)
}

func Install(options Options) error {
	if runtime.GOOS != "linux" {
		return errors.New("system installation currently supports Linux only")
	}
	if os.Geteuid() != 0 {
		return errors.New("install must be run as root (use sudo)")
	}
	if options.Server == "" || options.Token == "" {
		return errors.New("--server and --token are required")
	}
	if err := config.ValidateServerURL(options.Server); err != nil {
		return err
	}
	if options.ConfigPath == "" {
		options.ConfigPath = config.DefaultPath
	}
	if options.BinaryPath == "" {
		options.BinaryPath = DefaultBinaryPath
	}
	if options.UnitPath == "" {
		options.UnitPath = DefaultUnitPath
	}
	if options.StateDir == "" {
		options.StateDir = DefaultStateDir
	}
	if _, err := exec.LookPath("systemctl"); err != nil && options.StartService {
		return errors.New("systemctl is required")
	}
	if err := ensureUser(); err != nil {
		return err
	}
	if err := os.MkdirAll(options.StateDir, 0750); err != nil {
		return err
	}
	if err := chown(options.StateDir); err != nil {
		return err
	}
	credentials, err := register(options.Server, options.Token, "", options.Version)
	if err != nil {
		return err
	}
	if err := copyBinary(options.BinaryPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	cfg := &config.Config{Server: strings.TrimRight(options.Server, "/"), RunnerID: credentials.RunnerID, RunnerSecret: credentials.RunnerSecret, MaxConcurrentJobs: 1, CachePath: filepath.Join(options.StateDir, "executions.json"), Jobs: map[string]config.Job{}}
	if err := config.Save(options.ConfigPath, cfg); err != nil {
		return err
	}
	account, err := user.Lookup("cronpulser")
	if err != nil {
		return err
	}
	groupID, _ := strconv.Atoi(account.Gid)
	// Jobs run as the service account, so it may read its job allowlist but
	// must not be able to change it.
	if err := os.Chown(options.ConfigPath, 0, groupID); err != nil {
		return err
	}
	if err := os.Chmod(options.ConfigPath, 0640); err != nil {
		return err
	}
	if err := os.Chown(filepath.Dir(options.ConfigPath), 0, groupID); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(options.ConfigPath), 0750); err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=CronPulser Runner
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cronpulser
Group=cronpulser
ExecStart=%s run --config %s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, options.BinaryPath, options.ConfigPath)
	if err := os.WriteFile(options.UnitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if options.StartService {
		if err := command("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := command("systemctl", "enable", "--now", "cronpulser-runner.service"); err != nil {
			return err
		}
	}
	fmt.Printf("CronPulser Runner installed\nConfig: %s\nRunner ID: %s\n", options.ConfigPath, credentials.RunnerID)
	return nil
}

// Rotate consumes a one-time rotation token and replaces only the permanent
// credential in the existing config. Locally registered jobs and the execution
// cache remain unchanged.
func Rotate(options RotateOptions) error {
	if runtime.GOOS != "linux" {
		return errors.New("credential rotation currently supports Linux only")
	}
	if os.Geteuid() != 0 {
		return errors.New("rotate must be run as root (use sudo)")
	}
	if options.Token == "" {
		return errors.New("--token is required")
	}
	if options.ConfigPath == "" {
		options.ConfigPath = config.DefaultPath
	}
	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		return err
	}
	credentials, err := register(cfg.Server, options.Token, cfg.RunnerID, options.Version)
	if err != nil {
		return err
	}
	if credentials.RunnerID != cfg.RunnerID {
		return errors.New("rotation token belongs to a different runner")
	}
	cfg.RunnerSecret = credentials.RunnerSecret
	if err := config.Save(options.ConfigPath, cfg); err != nil {
		return fmt.Errorf("save rotated credentials: %w", err)
	}
	if options.RestartService {
		if _, err := exec.LookPath("systemctl"); err != nil {
			return errors.New("systemctl is required")
		}
		if err := command("systemctl", "restart", "cronpulser-runner.service"); err != nil {
			return err
		}
	}
	fmt.Printf("CronPulser Runner credentials rotated\nRunner ID: %s\n", cfg.RunnerID)
	return nil
}

func Status() error {
	return command("systemctl", "is-active", "cronpulser-runner.service")
}
