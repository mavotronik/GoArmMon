package scheduler_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goarmmon/internal/alerts"
	"goarmmon/internal/checks"
	"goarmmon/internal/config"
	"goarmmon/internal/scheduler"
	"goarmmon/internal/state"

	"gopkg.in/yaml.v3"
)

func init() {
	checks.Register("slowtest", newSlowRunner)
}

type slowRunner struct {
	host     string
	block    time.Duration
	interval time.Duration
}

func newSlowRunner(host config.HostConfig, raw yaml.Node) (checks.Runner, error) {
	var cfg struct {
		Block    string `yaml:"block"`
		Interval string `yaml:"interval"`
	}
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	block, err := time.ParseDuration(cfg.Block)
	if err != nil || block <= 0 {
		block = 2 * time.Second
	}
	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil || interval <= 0 {
		interval = time.Hour
	}
	return &slowRunner{
		host:     host.Name,
		block:    block,
		interval: interval,
	}, nil
}

func (r *slowRunner) ID() string               { return checks.CheckID(r.host, "slowtest") }
func (r *slowRunner) Type() string             { return "slowtest" }
func (r *slowRunner) HostName() string         { return r.host }
func (r *slowRunner) Interval() time.Duration  { return r.interval }

func (r *slowRunner) Run(ctx context.Context) state.CheckSnapshot {
	select {
	case <-ctx.Done():
		return state.CheckSnapshot{
			HostName:  r.host,
			CheckType: "slowtest",
			CheckID:   r.ID(),
			Status:    state.StatusUnknown,
			UpdatedAt: time.Now(),
		}
	case <-time.After(r.block):
		return state.CheckSnapshot{
			HostName:  r.host,
			CheckType: "slowtest",
			CheckID:   r.ID(),
			Status:    state.StatusOnline,
			UpdatedAt: time.Now(),
		}
	}
}

func loadSlowTestConfig(t *testing.T, hostName, block string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := fmt.Sprintf(`telegram:
  token: test
  allowed_users:
    - 1
logging:
  level: INFO
hosts:
  - name: %s
    checks:
      - type: slowtest
        block: %s
        interval: 1h
`, hostName, block)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestApplyConfigDoesNotWaitForInFlightChecks(t *testing.T) {
	cache := state.NewCache()
	alertMgr := alerts.NewManager(nil, 8)
	s := scheduler.New(cache, alertMgr)

	cfg := loadSlowTestConfig(t, "slow", "2s")
	ctx := context.Background()

	s.ApplyConfig(ctx, cfg)

	start := time.Now()
	s.ApplyConfig(ctx, cfg)
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("second ApplyConfig took %v, want < 200ms", elapsed)
	}

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return within 3s")
	}
}

func TestApplyConfigStaleCheckDoesNotUpdateCache(t *testing.T) {
	cache := state.NewCache()
	alertMgr := alerts.NewManager(nil, 8)
	s := scheduler.New(cache, alertMgr)

	cfgOld := loadSlowTestConfig(t, "old", "1h")
	ctx := context.Background()
	s.ApplyConfig(ctx, cfgOld)

	// Give the first check goroutine time to enter Run().
	time.Sleep(50 * time.Millisecond)

	cfgNew := loadSlowTestConfig(t, "new", "50ms")
	s.ApplyConfig(ctx, cfgNew)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := cache.GetCheck("new:slowtest"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, ok := cache.GetCheck("old:slowtest"); ok {
		t.Fatal("stale generation updated cache for removed host")
	}
	if _, ok := cache.GetCheck("new:slowtest"); !ok {
		t.Fatal("expected new generation to publish check snapshot")
	}

	s.Stop()
}
