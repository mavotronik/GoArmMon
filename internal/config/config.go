package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Logging  LoggingConfig  `yaml:"logging"`
	Hosts    []HostConfig   `yaml:"hosts"`
}

type TelegramConfig struct {
	Token        string  `yaml:"token"`
	AllowedUsers []int64 `yaml:"allowed_users"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

type HostConfig struct {
	Name               string                 `yaml:"name"`
	Description        string                 `yaml:"description"`
	Group              string                 `yaml:"group"`
	SkipOnPingFailure  bool                   `yaml:"skip_on_ping_failure"`
	PingFailThreshold  int                    `yaml:"-"`
	Alerts             HostAlerts             `yaml:"alerts"`
	Checks             []yaml.Node            `yaml:"checks"`
	rawChecks          []CheckDefinition
}

type HostAlerts struct {
	For            *string        `yaml:"for"`
	CPU            *Threshold     `yaml:"cpu"`
	RAM            *Threshold     `yaml:"ram"`
	Swap           *Threshold     `yaml:"swap"`
	Disk           *DiskThreshold `yaml:"disk"`
	RTT            *Threshold     `yaml:"rtt"`
	HTTPResponse   *Threshold     `yaml:"http_response"`
}

type Threshold struct {
	Warning  *float64 `yaml:"warning"`
	Critical *float64 `yaml:"critical"`
	Recovery *float64 `yaml:"recovery"`
	For      *string  `yaml:"for"`
}

type CheckDefinition struct {
	Type string
	Raw  yaml.Node
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

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required")
	}
	if len(c.Telegram.AllowedUsers) == 0 {
		return fmt.Errorf("telegram.allowed_users must not be empty")
	}
	if len(c.Hosts) == 0 {
		return fmt.Errorf("at least one host is required")
	}

	names := make(map[string]struct{}, len(c.Hosts))
	for i := range c.Hosts {
		h := &c.Hosts[i]
		if h.Name == "" {
			return fmt.Errorf("host[%d]: name is required", i)
		}
		if _, ok := names[h.Name]; ok {
			return fmt.Errorf("host %q: duplicate name", h.Name)
		}
		names[h.Name] = struct{}{}

		if len(h.Checks) == 0 {
			return fmt.Errorf("host %q: at least one check is required", h.Name)
		}

		h.rawChecks = make([]CheckDefinition, 0, len(h.Checks))
		for j, node := range h.Checks {
			var meta struct {
				Type string `yaml:"type"`
			}
			if err := node.Decode(&meta); err != nil {
				return fmt.Errorf("host %q check[%d]: %w", h.Name, j, err)
			}
			if meta.Type == "" {
				return fmt.Errorf("host %q check[%d]: type is required", h.Name, j)
			}
			h.rawChecks = append(h.rawChecks, CheckDefinition{
				Type: meta.Type,
				Raw:  node,
			})
		}

		h.resolvePingFailThreshold()
	}

	return nil
}

func (h *HostConfig) CheckDefinitions() []CheckDefinition {
	return h.rawChecks
}

func (h *HostConfig) resolvePingFailThreshold() {
	h.PingFailThreshold = 1
	for _, def := range h.rawChecks {
		if def.Type != "ping" {
			continue
		}
		var cfg struct {
			FailThreshold int `yaml:"fail_threshold"`
		}
		if err := def.Raw.Decode(&cfg); err != nil {
			continue
		}
		if cfg.FailThreshold > 0 {
			h.PingFailThreshold = cfg.FailThreshold
		}
		return
	}
}

func ParseDuration(s string, field string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", field, s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive", field)
	}
	return d, nil
}

func (t *Threshold) RecoveryForWarning() float64 {
	if t.Recovery != nil {
		return *t.Recovery
	}
	if t.Warning != nil {
		return *t.Warning
	}
	return 0
}

func (t *Threshold) RecoveryForCritical() float64 {
	if t.Recovery != nil {
		return *t.Recovery
	}
	if t.Critical != nil {
		return *t.Critical - 10
	}
	return 0
}

func (t *Threshold) Enabled() bool {
	return t != nil && (t.Warning != nil || t.Critical != nil)
}

// Forbearance returns how long a metric must stay above threshold before alerting.
// Per-metric "for" overrides the host-level default.
func (t *Threshold) Forbearance(defaultFor *string) (time.Duration, error) {
	s := defaultFor
	if t != nil && t.For != nil {
		s = t.For
	}
	if s == nil || *s == "" {
		return 0, nil
	}
	return ParseDuration(*s, "for")
}
