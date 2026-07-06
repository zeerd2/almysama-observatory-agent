package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	Server                   string `json:"server"`
	InstallToken             string `json:"install_token,omitempty"`
	AgentID                  string `json:"agent_id,omitempty"`
	AgentSecret              string `json:"agent_secret,omitempty"`
	Name                     string `json:"name,omitempty"`
	ReportIntervalSeconds    int    `json:"report_interval_seconds"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	InsecureTLS              bool   `json:"insecure_tls,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Defaults()
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	cfg.Defaults()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c *Config) Defaults() {
	if c.ReportIntervalSeconds <= 0 {
		c.ReportIntervalSeconds = 120
	}
	if c.HeartbeatIntervalSeconds <= 0 {
		c.HeartbeatIntervalSeconds = 30
	}
}
