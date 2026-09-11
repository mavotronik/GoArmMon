package telegram

import (
	"testing"
	"time"

	"goarmmon/internal/hostpause"
)

func TestFormatPauseRemaining(t *testing.T) {
	until := time.Now().Add(12 * time.Minute)
	p := hostpause.Pause{Preset: hostpause.Preset15m, Until: &until}
	if got := formatPauseRemaining(p); got != "12m" && got != "13m" {
		t.Fatalf("remaining = %q, want ~12m", got)
	}

	forever := hostpause.Pause{Preset: hostpause.PresetForever}
	if got := formatPauseRemaining(forever); got != "forever" {
		t.Fatalf("remaining = %q, want forever", got)
	}
}

func TestFormatPauseStatusText(t *testing.T) {
	until := time.Now().Add(2 * time.Hour)
	p := hostpause.Pause{Preset: hostpause.Preset3h, Until: &until}
	got := formatPauseStatusText(p)
	if got != "paused · 2h left" {
		t.Fatalf("status = %q, want paused · 2h left", got)
	}

	forever := hostpause.Pause{Preset: hostpause.PresetForever}
	if got := formatPauseStatusText(forever); got != "paused forever" {
		t.Fatalf("status = %q, want paused forever", got)
	}
}

func TestPauseButtonLabel(t *testing.T) {
	store := openTestPauseStore(t)
	b := &Bot{pauses: store}

	if got := b.pauseButtonLabel("web-01"); got != "Checks: ON" {
		t.Fatalf("label = %q, want Checks: ON", got)
	}

	until := time.Now().Add(5 * time.Minute)
	if err := store.Set("web-01", hostpause.Preset5m, &until); err != nil {
		t.Fatal(err)
	}
	got := b.pauseButtonLabel("web-01")
	if got != "Checks: OFF · 5m" && got != "Checks: OFF · 4m" {
		t.Fatalf("label = %q, want Checks: OFF · ~5m", got)
	}
}

func openTestPauseStore(t *testing.T) *hostpause.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := hostpause.Open(dir + "/acl.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
