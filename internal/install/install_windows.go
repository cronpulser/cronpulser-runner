//go:build windows

package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	DefaultUnitPath = "CronPulserRunner"
	serviceName     = "CronPulserRunner"
)

var (
	DefaultBinaryPath = filepath.Join(programFiles(), "CronPulser", "cronpulser-runner.exe")
	DefaultStateDir   = filepath.Join(programData(), "CronPulser", "state")
)

func programData() string {
	if value := os.Getenv("ProgramData"); value != "" {
		return value
	}
	return `C:\ProgramData`
}

func programFiles() string {
	if value := os.Getenv("ProgramFiles"); value != "" {
		return value
	}
	return `C:\Program Files`
}

func requireAdministrator() error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	defer token.Close()
	if !token.IsElevated() {
		return errors.New("install must be run from an elevated Administrator terminal")
	}
	return nil
}

func copyBinary(destination string) error {
	source, err := currentExecutable()
	if err != nil {
		return err
	}
	if strings.EqualFold(source, destination) {
		return nil
	}
	return copyFile(source, destination)
}

func grantServiceAccess(configDirectory, stateDirectory string) error {
	for _, arguments := range [][]string{
		{configDirectory, "/inheritance:r", "/grant:r", `*S-1-5-18:(OI)(CI)F`, `*S-1-5-32-544:(OI)(CI)F`, `*S-1-5-19:(OI)(CI)RX`},
		{stateDirectory, "/inheritance:r", "/grant:r", `*S-1-5-18:(OI)(CI)F`, `*S-1-5-32-544:(OI)(CI)F`, `*S-1-5-19:(OI)(CI)M`},
	} {
		output, err := exec.Command("icacls.exe", arguments...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("secure runner files: icacls.exe: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func Install(options Options) error {
	if err := requireAdministrator(); err != nil {
		return err
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
	if options.StateDir == "" {
		options.StateDir = DefaultStateDir
	}
	if err := os.MkdirAll(options.StateDir, 0750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.ConfigPath), 0750); err != nil {
		return err
	}
	// Lock the directories down before the permanent secret is written, so the
	// config never inherits the broader ProgramData permissions.
	if err := grantServiceAccess(filepath.Dir(options.ConfigPath), options.StateDir); err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer manager.Disconnect()
	if existing, openErr := manager.OpenService(serviceName); openErr == nil {
		existing.Close()
		return errors.New("CronPulser Runner service is already installed")
	}
	credentials, err := register(options.Server, options.Token, "", options.Version)
	if err != nil {
		return err
	}
	if err := copyBinary(options.BinaryPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	cfg := &config.Config{
		Server: strings.TrimRight(options.Server, "/"), RunnerID: credentials.RunnerID,
		RunnerSecret: credentials.RunnerSecret, MaxConcurrentJobs: 1,
		CachePath: filepath.Join(options.StateDir, "executions.json"), Jobs: map[string]config.Job{},
	}
	if err := config.Save(options.ConfigPath, cfg); err != nil {
		return err
	}
	_ = eventlog.Remove(serviceName)
	if err := eventlog.InstallAsEventCreate(serviceName, eventlog.Info|eventlog.Warning|eventlog.Error); err != nil {
		return fmt.Errorf("install Windows event source: %w", err)
	}
	service, err := manager.CreateService(serviceName, options.BinaryPath, mgr.Config{
		DisplayName: "CronPulser Runner", Description: "Executes approved local jobs dispatched by CronPulser",
		StartType: mgr.StartAutomatic, ServiceStartName: `NT AUTHORITY\LocalService`,
	}, "run", "--config", options.ConfigPath)
	if err != nil {
		return fmt.Errorf("create Windows service: %w", err)
	}
	defer service.Close()
	if err := service.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}, 86400); err != nil {
		return fmt.Errorf("configure service recovery: %w", err)
	}
	if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("enable service recovery: %w", err)
	}
	if options.StartService {
		if err := service.Start(); err != nil {
			return fmt.Errorf("start Windows service: %w", err)
		}
		if err := waitForServiceState(service, svc.Running, 20*time.Second); err != nil {
			return fmt.Errorf("wait for Windows service: %w", err)
		}
	}
	fmt.Printf("CronPulser Runner installed\nConfig: %s\nRunner ID: %s\n", options.ConfigPath, credentials.RunnerID)
	return nil
}

func Rotate(options RotateOptions) error {
	if err := requireAdministrator(); err != nil {
		return err
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
		return restartService()
	}
	return nil
}

// Upgrade replaces the installed binary with the running one. Windows cannot
// overwrite a running executable, so the service is stopped for the swap. The
// config, credentials, jobs and execution cache are not touched. If the
// service does not stay running, the previous binary is restored.
func Upgrade(options UpgradeOptions) error {
	if err := requireAdministrator(); err != nil {
		return err
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
	if strings.EqualFold(source, options.BinaryPath) {
		return errors.New("run upgrade from the downloaded binary, not the installed one")
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("CronPulser Runner service is not installed; use install instead: %w", err)
	}
	defer service.Close()
	previousVersion := installedVersion(options.BinaryPath)
	if options.RestartService {
		if err := stopService(service); err != nil {
			return fmt.Errorf("stop Windows service: %w", err)
		}
	}
	previous := previousBinaryPath(options.BinaryPath)
	if err := copyFile(options.BinaryPath, previous); err != nil {
		return fmt.Errorf("keep previous binary: %w", err)
	}
	if err := copyFile(source, options.BinaryPath); err != nil {
		if options.RestartService {
			_ = service.Start()
		}
		return fmt.Errorf("install binary: %w", err)
	}
	if options.RestartService {
		if err := startAndWatch(service); err != nil {
			_ = stopService(service)
			if restoreErr := copyFile(previous, options.BinaryPath); restoreErr != nil {
				return fmt.Errorf("%w; restoring the previous binary also failed: %v", err, restoreErr)
			}
			if startErr := service.Start(); startErr != nil {
				return fmt.Errorf("%w; previous binary restored but the service did not start: %v", err, startErr)
			}
			return fmt.Errorf("%w; previous version %s restored and started (check the Windows Application event log)", err, previousVersion)
		}
	}
	fmt.Printf("CronPulser Runner upgraded\nVersion: %s -> %s\nConfig unchanged: %s\nPrevious binary: %s\n", previousVersion, options.Version, options.ConfigPath, previous)
	return nil
}

func stopService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil {
		return err
	}
	return waitForServiceState(service, svc.Stopped, 20*time.Second)
}

func startAndWatch(service *mgr.Service) error {
	if err := service.Start(); err != nil {
		return err
	}
	if err := waitForServiceState(service, svc.Running, 20*time.Second); err != nil {
		return err
	}
	deadline := time.Now().Add(upgradeHealthWindow)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State != svc.Running {
			return errors.New("the upgraded service did not stay running")
		}
	}
	return nil
}

func restartService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State != svc.Stopped {
		if _, err := service.Control(svc.Stop); err != nil {
			return err
		}
		if err := waitForServiceState(service, svc.Stopped, 20*time.Second); err != nil {
			return err
		}
	}
	if err := service.Start(); err != nil {
		return err
	}
	return waitForServiceState(service, svc.Running, 20*time.Second)
}

func waitForServiceState(service *mgr.Service, expected svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == expected {
			return nil
		}
		if expected == svc.Running && status.State == svc.Stopped {
			return errors.New("service stopped before reaching the running state; check the Windows Application event log")
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for service state %s", serviceState(expected))
}

func Status() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	fmt.Printf("CronPulser Runner service: %s\n", serviceState(status.State))
	if status.State != svc.Running {
		return errors.New("service is not running")
	}
	return nil
}

func serviceState(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "starting"
	case svc.StopPending:
		return "stopping"
	case svc.Running:
		return "running"
	default:
		return fmt.Sprintf("state %d", state)
	}
}
