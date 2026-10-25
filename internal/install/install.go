//go:build !windows

package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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
	source, err := currentExecutable()
	if err != nil {
		return err
	}
	if source == destination {
		return nil
	}
	return copyFile(source, destination)
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

// Upgrade replaces the installed binary with the running one and restarts the
// service. The config, credentials, jobs and execution cache are not touched.
// If the service does not stay running, the previous binary is restored.
func Upgrade(options UpgradeOptions) error {
	if runtime.GOOS != "linux" {
		return errors.New("upgrade currently supports Linux only")
	}
	if os.Geteuid() != 0 {
		return errors.New("upgrade must be run as root (use sudo)")
	}
	if options.ConfigPath == "" {
		options.ConfigPath = config.DefaultPath
	}
	if options.BinaryPath == "" {
		options.BinaryPath = DefaultBinaryPath
	}
	if _, err := os.Stat(options.BinaryPath); err != nil {
		return fmt.Errorf("no installed Runner at %s; use install instead: %w", options.BinaryPath, err)
	}
	// The new binary must accept the existing config before anything changes.
	if _, err := config.Load(options.ConfigPath); err != nil {
		return fmt.Errorf("existing config is not valid for version %s, nothing was changed: %w", options.Version, err)
	}
	source, err := currentExecutable()
	if err != nil {
		return err
	}
	if source == options.BinaryPath {
		return errors.New("run upgrade from the downloaded binary, not the installed one")
	}
	if options.RestartService {
		if _, err := exec.LookPath("systemctl"); err != nil {
			return errors.New("systemctl is required")
		}
	}
	previousVersion := installedVersion(options.BinaryPath)
	previous := previousBinaryPath(options.BinaryPath)
	if err := copyFile(options.BinaryPath, previous); err != nil {
		return fmt.Errorf("keep previous binary: %w", err)
	}
	if err := copyFile(source, options.BinaryPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	if options.RestartService {
		if err := restartAndWatch(); err != nil {
			if restoreErr := copyFile(previous, options.BinaryPath); restoreErr != nil {
				return fmt.Errorf("%w; restoring the previous binary also failed: %v", err, restoreErr)
			}
			if restartErr := command("systemctl", "restart", "cronpulser-runner.service"); restartErr != nil {
				return fmt.Errorf("%w; previous binary restored but the service did not restart: %v", err, restartErr)
			}
			return fmt.Errorf("%w; previous version %s restored and restarted (see journalctl -u cronpulser-runner)", err, previousVersion)
		}
	}
	fmt.Printf("CronPulser Runner upgraded\nVersion: %s -> %s\nConfig unchanged: %s\nPrevious binary: %s\n", previousVersion, options.Version, options.ConfigPath, previous)
	return nil
}

func restartAndWatch() error {
	if err := command("systemctl", "restart", "cronpulser-runner.service"); err != nil {
		return err
	}
	deadline := time.Now().Add(upgradeHealthWindow)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if err := exec.Command("systemctl", "is-active", "--quiet", "cronpulser-runner.service").Run(); err != nil {
			return errors.New("the upgraded service did not stay running")
		}
	}
	return nil
}

func Status() error {
	return command("systemctl", "is-active", "cronpulser-runner.service")
}
