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
	LLM            LLM    `yaml:"llm,omitempty"`
}

// LLM configures scraping of local inference runtimes (llama.cpp, vLLM) for
// KV cache, throughput, and queue telemetry.
type LLM struct {
	// Autodetect probes well-known local inference ports. Defaults to true
	// when the config file has no llm block at all, so existing installs
	// pick the feature up without being edited.
	Autodetect *bool         `yaml:"autodetect,omitempty"`
	Endpoints  []LLMEndpoint `yaml:"endpoints,omitempty"`
}

type LLMEndpoint struct {
	URL     string `yaml:"url"`               // base URL, e.g. http://127.0.0.1:8080
	Runtime string `yaml:"runtime,omitempty"` // auto (default) | llamacpp | vllm
	APIKey  string `yaml:"api_key,omitempty"` // optional bearer token for a protected /metrics
}

// AutodetectEnabled reports whether well-known ports should be probed,
// applying the default-on behaviour for configs that omit the setting.
func (l LLM) AutodetectEnabled() bool {
	return l.Autodetect == nil || *l.Autodetect
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
