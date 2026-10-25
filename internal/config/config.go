package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var DefaultPath = defaultConfigPath()

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!int" {
		seconds, err := strconv.Atoi(node.Value)
		if err != nil {
			return err
		}
		d.Duration = time.Duration(seconds) * time.Second
		return nil
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("use seconds or a duration such as 30s: %w", err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) { return int(d.Seconds()), nil }

type Job struct {
	Command          string   `yaml:"command"`
	Arguments        []string `yaml:"arguments,omitempty"`
	WorkingDirectory string   `yaml:"workingDirectory,omitempty"`
	Timeout          Duration `yaml:"timeout"`
}

type Config struct {
	Server            string         `yaml:"server"`
	RunnerID          string         `yaml:"runnerId"`
	RunnerSecret      string         `yaml:"runnerSecret"`
	MaxConcurrentJobs int            `yaml:"maxConcurrentJobs"`
	CachePath         string         `yaml:"cachePath"`
	Jobs              map[string]Job `yaml:"jobs"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, cfg.Validate()
}

func Save(path string, cfg *Config) error {
	applyDefaults(cfg)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxConcurrentJobs == 0 {
		cfg.MaxConcurrentJobs = 1
	}
	if cfg.CachePath == "" {
		cfg.CachePath = defaultCachePath()
	}
	if cfg.Jobs == nil {
		cfg.Jobs = make(map[string]Job)
	}
}

func (c *Config) Validate() error {
	if c.Server == "" || c.RunnerID == "" || c.RunnerSecret == "" {
		return errors.New("server, runnerId, and runnerSecret are required")
	}
	if err := ValidateServerURL(c.Server); err != nil {
		return err
	}
	if c.MaxConcurrentJobs != 1 {
		return errors.New("this release supports maxConcurrentJobs: 1")
	}
	for key, job := range c.Jobs {
		if key == "" || job.Command == "" {
			return fmt.Errorf("job %q must have a command", key)
		}
		if !filepath.IsAbs(job.Command) {
			return fmt.Errorf("job %q command must be an absolute path", key)
		}
		if job.Timeout.Duration <= 0 {
			return fmt.Errorf("job %q must have a positive timeout", key)
		}
		info, err := os.Stat(job.Command)
		if err != nil {
			return fmt.Errorf("job %q command: %w", key, err)
		}
		if err := validateExecutable(info); err != nil {
			return fmt.Errorf("job %q command is not executable: %w", key, err)
		}
		if job.WorkingDirectory != "" {
			info, err := os.Stat(job.WorkingDirectory)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("job %q workingDirectory is invalid", key)
			}
		}
	}
	return nil
}

func ValidateServerURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return errors.New("server must be a valid URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return nil
	}
	return errors.New("server must use https (http is allowed only for local development)")
}

func (c *Config) JobKeys() []string {
	keys := make([]string, 0, len(c.Jobs))
	for key := range c.Jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
