package hostpause

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "acl.sqlite")
	store, err := Open(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSetTimedAndGet(t *testing.T) {
	store := openTestStore(t)
	until := time.Now().Add(15 * time.Minute)
	if err := store.Set("web-01", Preset15m, &until); err != nil {
		t.Fatal(err)
	}

	p, ok := store.Get("web-01")
	if !ok {
		t.Fatal("expected pause")
	}
	if p.Preset != Preset15m {
		t.Fatalf("preset = %q, want %q", p.Preset, Preset15m)
	}
	if p.Until == nil || p.Until.Unix() != until.Unix() {
		t.Fatalf("until = %v, want %v", p.Until, until)
	}
	if !store.IsPaused("web-01") {
		t.Fatal("expected paused")
	}
}

func TestSetForever(t *testing.T) {
	store := openTestStore(t)
	if err := store.Set("db-01", PresetForever, nil); err != nil {
		t.Fatal(err)
	}
	p, ok := store.Get("db-01")
	if !ok {
		t.Fatal("expected pause")
	}
	if p.Until != nil {
		t.Fatalf("until = %v, want nil", p.Until)
	}
	if p.Preset != PresetForever {
		t.Fatalf("preset = %q", p.Preset)
	}
}

func TestClear(t *testing.T) {
	store := openTestStore(t)
	until := time.Now().Add(time.Hour)
	if err := store.Set("web-01", Preset3h, &until); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear("web-01"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("web-01"); ok {
		t.Fatal("expected no pause after clear")
	}
}

func TestExpiryOnGet(t *testing.T) {
	store := openTestStore(t)
	until := time.Now().Add(-time.Minute)
	if err := store.Set("web-01", Preset5m, &until); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("web-01"); ok {
		t.Fatal("expected expired pause to be inactive")
	}
	if store.IsPaused("web-01") {
		t.Fatal("expected not paused after expiry")
	}
}

func TestRename(t *testing.T) {
	store := openTestStore(t)
	until := time.Now().Add(time.Hour)
	if err := store.Set("old-name", Preset12h, &until); err != nil {
		t.Fatal(err)
	}
	if err := store.Rename("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("old-name"); ok {
		t.Fatal("old name should not have pause")
	}
	p, ok := store.Get("new-name")
	if !ok {
		t.Fatal("expected pause on new name")
	}
	if p.Preset != Preset12h {
		t.Fatalf("preset = %q", p.Preset)
	}
}

func TestPrune(t *testing.T) {
	store := openTestStore(t)
	until := time.Now().Add(time.Hour)
	if err := store.Set("keep", Preset5m, &until); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("remove", Preset5m, &until); err != nil {
		t.Fatal(err)
	}
	if err := store.Prune([]string{"keep"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("remove"); ok {
		t.Fatal("pruned host should have no pause")
	}
	if _, ok := store.Get("keep"); !ok {
		t.Fatal("kept host should still be paused")
	}
}

func TestSetFromPreset(t *testing.T) {
	store := openTestStore(t)
	before := time.Now()
	if err := store.SetFromPreset("web-01", Preset5m); err != nil {
		t.Fatal(err)
	}
	p, ok := store.Get("web-01")
	if !ok || p.Until == nil {
		t.Fatal("expected timed pause")
	}
	if p.Until.Before(before.Add(4*time.Minute)) || p.Until.After(before.Add(6*time.Minute)) {
		t.Fatalf("until %v outside expected 5m window from %v", p.Until, before)
	}
}

func TestReloadFromDB(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "acl.sqlite")

	until := time.Now().Add(time.Hour)
	store1, err := Open(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store1.Set("web-01", Preset24h, &until); err != nil {
		t.Fatal(err)
	}
	if err := store1.Close(); err != nil {
		t.Fatal(err)
	}

	store2, err := Open(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	p, ok := store2.Get("web-01")
	if !ok {
		t.Fatal("expected pause after reload")
	}
	if p.Preset != Preset24h {
		t.Fatalf("preset = %q", p.Preset)
	}
}
