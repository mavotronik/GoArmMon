package alerts

import (
	"testing"
	"time"

	"goarmmon/internal/config"
	"goarmmon/internal/state"
)

func floatPtr(v float64) *float64 { return &v }
func strPtr(v string) *string       { return &v }

func testHost(forDefault *string, cpu *config.Threshold) config.HostConfig {
	return config.HostConfig{
		Name: "server",
		Alerts: config.HostAlerts{
			For: forDefault,
			CPU: cpu,
		},
	}
}

func pingSnap(status state.Status) state.CheckSnapshot {
	return state.CheckSnapshot{
		HostName:  "server",
		CheckType: "ping",
		CheckID:   "server:ping",
		Status:    status,
	}
}

func pingHost(threshold int) config.HostConfig {
	h := config.HostConfig{
		Name:              "server",
		PingFailThreshold: threshold,
	}
	return h
}

func TestPingFailThresholdDelaysOfflineAlert(t *testing.T) {
	m := NewManager([]config.HostConfig{pingHost(3)}, 8)

	got := m.Evaluate(pingSnap(state.StatusOffline))
	if got.Status != state.StatusPartial {
		t.Fatalf("expected PARTIAL after 1 failure, got %s", got.Status)
	}
	if got.StatusLabel() != "PARTIAL (3/1)" {
		t.Fatalf("expected PARTIAL (3/1), got %q", got.StatusLabel())
	}

	got = m.Evaluate(pingSnap(state.StatusOffline))
	if got.Status != state.StatusPartial {
		t.Fatalf("expected PARTIAL after 2 failures, got %s", got.Status)
	}
	if got.StatusLabel() != "PARTIAL (3/2)" {
		t.Fatalf("expected PARTIAL (3/2), got %q", got.StatusLabel())
	}

	got = m.Evaluate(pingSnap(state.StatusOffline))
	if got.Status != state.StatusOffline {
		t.Fatalf("expected offline alert after threshold failures, got %s", got.Status)
	}
}

func TestPingPartialEmitsPartialNotOffline(t *testing.T) {
	m := NewManager([]config.HostConfig{pingHost(3)}, 8)

	m.Evaluate(pingSnap(state.StatusOffline))
	select {
	case ev := <-m.Events():
		if ev.Kind == EventOffline {
			t.Fatal("expected no offline event for partial ping failure")
		}
		if ev.Kind != EventPartial {
			t.Fatalf("expected partial event, got %v", ev.Kind)
		}
	default:
		t.Fatal("expected partial event")
	}
}

func TestPingFailThresholdResetsOnSuccess(t *testing.T) {
	m := NewManager([]config.HostConfig{pingHost(3)}, 8)

	m.Evaluate(pingSnap(state.StatusOffline))
	m.Evaluate(pingSnap(state.StatusOffline))
	got := m.Evaluate(pingSnap(state.StatusOnline))
	if got.Status != state.StatusOnline {
		t.Fatalf("expected online after success, got %s", got.Status)
	}

	got = m.Evaluate(pingSnap(state.StatusOffline))
	if got.Status != state.StatusPartial {
		t.Fatalf("expected fail streak to reset after success, got %s", got.Status)
	}
	if got.StatusLabel() != "PARTIAL (3/1)" {
		t.Fatalf("expected PARTIAL (3/1), got %q", got.StatusLabel())
	}
}

func glancesSnap(cpu float64) state.CheckSnapshot {
	return state.CheckSnapshot{
		HostName:  "server",
		CheckType: "glances",
		CheckID:   "server:glances",
		Status:    state.StatusOnline,
		Glances:   &state.GlancesData{CPU: cpu},
	}
}

func TestForbearanceDelaysAlert(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	m := NewManager([]config.HostConfig{testHost(strPtr("2m"), &config.Threshold{
		Warning: floatPtr(70),
	})}, 8)
	m.now = func() time.Time { return now }

	snap := glancesSnap(80)
	got := m.Evaluate(snap)
	if got.Status != state.StatusOnline {
		t.Fatalf("expected no alert during forbearance, got %s", got.Status)
	}

	now = now.Add(1 * time.Minute)
	got = m.Evaluate(glancesSnap(80))
	if got.Status != state.StatusOnline {
		t.Fatalf("expected no alert before forbearance elapsed, got %s", got.Status)
	}

	now = now.Add(1 * time.Minute)
	got = m.Evaluate(glancesSnap(80))
	if got.Status != state.StatusWarning {
		t.Fatalf("expected warning after forbearance, got %s", got.Status)
	}
}

func TestForbearanceResetsOnRecovery(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	m := NewManager([]config.HostConfig{testHost(strPtr("2m"), &config.Threshold{
		Warning:  floatPtr(70),
		Recovery: floatPtr(60),
	})}, 8)
	m.now = func() time.Time { return now }

	m.Evaluate(glancesSnap(80))
	now = now.Add(1 * time.Minute)
	m.Evaluate(glancesSnap(50))

	now = now.Add(30 * time.Second)
	got := m.Evaluate(glancesSnap(80))
	if got.Status != state.StatusOnline {
		t.Fatalf("expected forbearance timer to reset after recovery, got %s", got.Status)
	}
}

func TestImmediateAlertWithoutForbearance(t *testing.T) {
	m := NewManager([]config.HostConfig{testHost(nil, &config.Threshold{
		Warning: floatPtr(70),
	})}, 8)

	got := m.Evaluate(glancesSnap(80))
	if got.Status != state.StatusWarning {
		t.Fatalf("expected immediate warning, got %s", got.Status)
	}
}

func TestPerMetricForOverridesDefault(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	m := NewManager([]config.HostConfig{testHost(strPtr("5m"), &config.Threshold{
		Warning: floatPtr(70),
		For:     strPtr("30s"),
	})}, 8)
	m.now = func() time.Time { return now }

	m.Evaluate(glancesSnap(80))
	now = now.Add(30 * time.Second)
	got := m.Evaluate(glancesSnap(80))
	if got.Status != state.StatusWarning {
		t.Fatalf("expected per-metric for to apply, got %s", got.Status)
	}
}
