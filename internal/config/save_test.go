package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfigYAML = `telegram:
  token: "test-token"
  allowed_users:
    - 1
logging:
  level: INFO
hosts:
  - name: router
    description: "Home router"
    group: home
    skip_on_ping_failure: true
    alerts:
      rtt:
        warning: 100
        critical: 500
        recovery: 80
    checks:
      - type: ping
        address: 192.168.1.1
        timeout: 3s
        interval: 5s
        fail_threshold: 3
      - type: http
        url: http://192.168.1.1/
        method: GET
        expected_code: 200
        follow_redirects: true
        timeout: 5s
        interval: 30s
`

func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(testConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Hosts[0].Description = "Updated router"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Hosts[0].Description != "Updated router" {
		t.Fatalf("description = %q, want Updated router", reloaded.Hosts[0].Description)
	}

	ping, _, ok := GetPingCheck(&reloaded.Hosts[0])
	if !ok || ping.Address != "192.168.1.1" {
		t.Fatalf("ping check lost after save: ok=%v cfg=%+v", ok, ping)
	}
	httpCfg, _, ok := GetHTTPCheck(&reloaded.Hosts[0])
	if !ok || httpCfg.URL != "http://192.168.1.1/" {
		t.Fatalf("http check lost after save: ok=%v cfg=%+v", ok, httpCfg)
	}
}

func TestSaveRoundTripMessages(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Hosts[0].Messages = HostMessages{
		Offline:  "down",
		Online:   "up",
		Warning:  "warn",
		Critical: "crit",
		Recovery: "ok",
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Hosts[0].Messages
	if got.Offline != "down" || got.Online != "up" || got.Warning != "warn" || got.Critical != "crit" || got.Recovery != "ok" {
		t.Fatalf("messages = %+v", got)
	}
}

func TestClonePreservesHosts(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	clone, err := Clone(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clone.Hosts[0].Name = "changed"

	if cfg.Hosts[0].Name == "changed" {
		t.Fatal("clone mutated original")
	}
}

func TestDefaultHostHasPingCheck(t *testing.T) {
	h := DefaultHost("newhost")
	if h.Name != "newhost" {
		t.Fatalf("name = %q", h.Name)
	}
	ping, _, ok := GetPingCheck(&h)
	if !ok {
		t.Fatal("expected default ping check")
	}
	if ping.Timeout != "3s" || ping.Interval != "5s" || ping.FailThreshold != 3 {
		t.Fatalf("unexpected ping defaults: %+v", ping)
	}
	if h.Alerts.RTT == nil || h.Alerts.RTT.Warning == nil || *h.Alerts.RTT.Warning != 100 {
		t.Fatalf("expected default rtt alert: %+v", h.Alerts.RTT)
	}
}

func TestSaveRequiresValidation(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hosts = nil
	if err := Save(path, cfg); err == nil {
		t.Fatal("expected validation error for empty hosts")
	}
}
