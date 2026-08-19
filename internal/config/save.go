package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Clone returns a deep copy of cfg via YAML round-trip.
func Clone(cfg *Config) (*Config, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal clone: %w", err)
	}
	var out Config
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal clone: %w", err)
	}
	return &out, nil
}

// Save validates and atomically writes cfg to path.
func Save(path string, cfg *Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// HostByIndex returns the host at index i.
func (c *Config) HostByIndex(i int) (*HostConfig, bool) {
	if i < 0 || i >= len(c.Hosts) {
		return nil, false
	}
	return &c.Hosts[i], true
}

// HostIndex returns the index of a host by name.
func (c *Config) HostIndex(name string) (int, bool) {
	for i := range c.Hosts {
		if c.Hosts[i].Name == name {
			return i, true
		}
	}
	return -1, false
}

// DefaultHost returns a new host with sensible defaults for further editing.
func DefaultHost(name string) HostConfig {
	w100, w500, w80 := 100.0, 500.0, 80.0
	h := HostConfig{
		Name:              name,
		SkipOnPingFailure: true,
		Alerts: HostAlerts{
			RTT: &Threshold{
				Warning:  &w100,
				Critical: &w500,
				Recovery: &w80,
			},
		},
	}
	_ = SetPingCheck(&h, PingCheckConfig{
		Type:          "ping",
		Timeout:       "3s",
		Interval:      "5s",
		FailThreshold: 3,
	})
	return h
}
