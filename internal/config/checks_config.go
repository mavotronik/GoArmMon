package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type PingCheckConfig struct {
	Type          string `yaml:"type"`
	Address       string `yaml:"address"`
	Timeout       string `yaml:"timeout"`
	Interval      string `yaml:"interval"`
	FailThreshold int    `yaml:"fail_threshold,omitempty"`
}

type HTTPCheckConfig struct {
	Type            string `yaml:"type"`
	URL             string `yaml:"url"`
	Method          string `yaml:"method,omitempty"`
	ExpectedCode    int    `yaml:"expected_code,omitempty"`
	Timeout         string `yaml:"timeout"`
	Interval        string `yaml:"interval"`
	FollowRedirects bool   `yaml:"follow_redirects,omitempty"`
}

type GlancesCheckConfig struct {
	Type       string `yaml:"type"`
	URL        string `yaml:"url"`
	APIVersion string `yaml:"api_version,omitempty"`
	Username   string `yaml:"username,omitempty"`
	Password   string `yaml:"password,omitempty"`
	Token      string `yaml:"token,omitempty"`
	Timeout    string `yaml:"timeout"`
	Interval   string `yaml:"interval"`
}

func ListCheckTypes(h HostConfig) []string {
	types := make([]string, 0, len(h.Checks))
	for _, node := range h.Checks {
		var meta struct {
			Type string `yaml:"type"`
		}
		if err := node.Decode(&meta); err != nil || meta.Type == "" {
			continue
		}
		types = append(types, meta.Type)
	}
	return types
}

func GetPingCheck(h *HostConfig) (*PingCheckConfig, int, bool) {
	return getCheck[PingCheckConfig](h, "ping")
}

func GetHTTPCheck(h *HostConfig) (*HTTPCheckConfig, int, bool) {
	return getCheck[HTTPCheckConfig](h, "http")
}

func GetGlancesCheck(h *HostConfig) (*GlancesCheckConfig, int, bool) {
	return getCheck[GlancesCheckConfig](h, "glances")
}

func getCheck[T any](h *HostConfig, checkType string) (*T, int, bool) {
	for i, node := range h.Checks {
		var meta struct {
			Type string `yaml:"type"`
		}
		if err := node.Decode(&meta); err != nil || meta.Type != checkType {
			continue
		}
		var cfg T
		if err := node.Decode(&cfg); err != nil {
			return nil, i, false
		}
		return &cfg, i, true
	}
	return nil, -1, false
}

func SetPingCheck(h *HostConfig, cfg PingCheckConfig) error {
	cfg.Type = "ping"
	return setCheck(h, "ping", cfg)
}

func SetHTTPCheck(h *HostConfig, cfg HTTPCheckConfig) error {
	cfg.Type = "http"
	return setCheck(h, "http", cfg)
}

func SetGlancesCheck(h *HostConfig, cfg GlancesCheckConfig) error {
	cfg.Type = "glances"
	return setCheck(h, "glances", cfg)
}

func setCheck(h *HostConfig, checkType string, cfg any) error {
	node, err := encodeCheck(cfg)
	if err != nil {
		return err
	}
	for i, existing := range h.Checks {
		var meta struct {
			Type string `yaml:"type"`
		}
		if err := existing.Decode(&meta); err != nil {
			continue
		}
		if meta.Type == checkType {
			h.Checks[i] = node
			return nil
		}
	}
	h.Checks = append(h.Checks, node)
	return nil
}

func RemoveCheck(h *HostConfig, checkType string) bool {
	out := h.Checks[:0]
	removed := false
	for _, node := range h.Checks {
		var meta struct {
			Type string `yaml:"type"`
		}
		if err := node.Decode(&meta); err != nil {
			out = append(out, node)
			continue
		}
		if meta.Type == checkType {
			removed = true
			continue
		}
		out = append(out, node)
	}
	h.Checks = out
	return removed
}

func HasCheck(h HostConfig, checkType string) bool {
	for _, node := range h.Checks {
		var meta struct {
			Type string `yaml:"type"`
		}
		if err := node.Decode(&meta); err != nil {
			continue
		}
		if meta.Type == checkType {
			return true
		}
	}
	return false
}

func encodeCheck(v any) (yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshal check: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return yaml.Node{}, fmt.Errorf("unmarshal check node: %w", err)
	}
	if len(doc.Content) == 0 || len(doc.Content[0].Content) == 0 {
		return yaml.Node{}, fmt.Errorf("empty check node")
	}
	return *doc.Content[0], nil
}

func DefaultHTTPCheck() HTTPCheckConfig {
	return HTTPCheckConfig{
		Type:            "http",
		Method:          "GET",
		ExpectedCode:    200,
		FollowRedirects: true,
		Timeout:         "5s",
		Interval:        "30s",
	}
}

func DefaultGlancesCheck() GlancesCheckConfig {
	return GlancesCheckConfig{
		Type:       "glances",
		APIVersion: "auto",
		Timeout:    "10s",
		Interval:   "15s",
	}
}
