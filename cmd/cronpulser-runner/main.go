package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cronpulser/cronpulser-runner/internal/cache"
	"github.com/cronpulser/cronpulser-runner/internal/client"
	"github.com/cronpulser/cronpulser-runner/internal/config"
	installer "github.com/cronpulser/cronpulser-runner/internal/install"
	"github.com/cronpulser/cronpulser-runner/internal/runservice"
)

var version = "dev"

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: cronpulser-runner <install|rotate|run|validate|status|version> [options]")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = installCommand(os.Args[2:])
	case "rotate":
		err = rotateCommand(os.Args[2:])
	case "run":
		err = runCommand(os.Args[2:])
	case "validate":
		err = validateCommand(os.Args[2:])
	case "status":
		err = statusCommand()
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Printf("error=%q", err)
		os.Exit(1)
	}
}

func rotateCommand(arguments []string) error {
	flags := flag.NewFlagSet("rotate", flag.ContinueOnError)
	token := flags.String("token", "", "one-time credential rotation token")
	configPath := flags.String("config", config.DefaultPath, "configuration path")
	skipService := flags.Bool("skip-service", false, "update credentials without restarting systemd (development only)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	return installer.Rotate(installer.RotateOptions{Token: *token, ConfigPath: *configPath, Version: version, RestartService: !*skipService})
}

func installCommand(arguments []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	server := flags.String("server", "", "CronPulser API URL")
	token := flags.String("token", "", "one-time installation token")
	configPath := flags.String("config", config.DefaultPath, "configuration path")
	binaryPath := flags.String("binary-destination", installer.DefaultBinaryPath, "installed binary path")
	unitPath := flags.String("systemd-unit", installer.DefaultUnitPath, "systemd unit path")
	stateDir := flags.String("state-dir", installer.DefaultStateDir, "persistent state directory")
	skipService := flags.Bool("skip-service", false, "write files without enabling systemd (development only)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	return installer.Install(installer.Options{Server: *server, Token: *token, ConfigPath: *configPath, BinaryPath: *binaryPath, UnitPath: *unitPath, StateDir: *stateDir, Version: version, StartService: !*skipService})
}

func runCommand(arguments []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultPath, "configuration path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	executionCache, err := cache.Load(cfg.CachePath)
	if err != nil {
		return err
	}
	return runservice.Run(func(ctx context.Context) error {
		log.Printf("event=runner_started version=%q jobs=%d", version, len(cfg.Jobs))
		return client.New(cfg, executionCache, version).Run(ctx)
	})
}

func validateCommand(arguments []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := flags.String("config", config.DefaultPath, "configuration path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("Configuration valid\n\nServer:\n%s\n\nRunner:\n%s\n\nJobs:\n%d\n", cfg.Server, cfg.RunnerID, len(cfg.Jobs))
	for _, key := range cfg.JobKeys() {
		fmt.Printf("%-24s OK\n", key)
	}
	return nil
}

func statusCommand() error {
	return installer.Status()
}
