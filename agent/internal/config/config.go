package config

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerURL      string `yaml:"server_url"`
	HostID         string `yaml:"host_id"`
	APIKey         string `yaml:"api_key"`
	IntervalSecs   int    `yaml:"interval_seconds"`
	InsecureSkipTLS bool  `yaml:"insecure_skip_tls_verify,omitempty"`
}

// Path returns the platform-appropriate config file location.
func Path() string {
	if runtime.GOOS == "windows" {
		root := os.Getenv("ProgramData")
		if root == "" {
			root = `C:\ProgramData`
		}
		return filepath.Join(root, "SentinelAgent", "config.yaml")
	}
	return "/etc/sentinel-agent/config.yaml"
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.IntervalSecs <= 0 {
		cfg.IntervalSecs = 10
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
