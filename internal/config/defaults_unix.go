//go:build !windows

package config

import (
	"os"
)

func defaultConfigPath() string { return "/etc/cronpulser/runner.yaml" }

func defaultCachePath() string { return "/var/lib/cronpulser/executions.json" }

func validateExecutable(info os.FileInfo) error {
	if info.IsDir() || info.Mode().Perm()&0111 == 0 {
		return os.ErrPermission
	}
	return nil
}
