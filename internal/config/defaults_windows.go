//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func programData() string {
	if value := os.Getenv("ProgramData"); value != "" {
		return value
	}
	return `C:\ProgramData`
}

func defaultConfigPath() string {
	return filepath.Join(programData(), "CronPulser", "runner.yaml")
}

func defaultCachePath() string {
	return filepath.Join(programData(), "CronPulser", "executions.json")
}

func validateExecutable(info os.FileInfo) error {
	if info.IsDir() {
		return errors.New("command is a directory")
	}
	ext := strings.ToLower(filepath.Ext(info.Name()))
	if ext != ".exe" && ext != ".com" {
		return errors.New("unsupported Windows executable type")
	}
	return nil
}
