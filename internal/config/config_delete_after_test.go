package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteAfterDurationDefault(t *testing.T) {
	t.Parallel()
	d, err := (TelegramConfig{}).DeleteAfterDuration()
	if err != nil {
		t.Fatal(err)
	}
	if d != DefaultDeleteAfter {
		t.Fatalf("default = %v, want %v", d, DefaultDeleteAfter)
	}
}

func TestDeleteAfterDurationCustom(t *testing.T) {
	t.Parallel()
	d, err := (TelegramConfig{DeleteAfter: "24h"}).DeleteAfterDuration()
	if err != nil {
		t.Fatal(err)
	}
	if d != 24*time.Hour {
		t.Fatalf("duration = %v", d)
	}
}

func TestDeleteAfterDurationDisabled(t *testing.T) {
	t.Parallel()
	d, err := (TelegramConfig{DeleteAfter: "0s"}).DeleteAfterDuration()
	if err != nil {
		t.Fatal(err)
	}
	if d != 0 {
		t.Fatalf("duration = %v, want 0", d)
	}
}

func TestDeleteAfterDurationTooLong(t *testing.T) {
	t.Parallel()
	_, err := (TelegramConfig{DeleteAfter: "72h"}).DeleteAfterDuration()
	if err == nil {
		t.Fatal("expected error for 72h")
	}
}

func TestLoadDeleteAfterValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	write := func(yaml string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(`telegram:
  token: "test-token"
  allowed_users:
    - 1
  delete_after: 72h
logging:
  level: INFO
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for 72h")
	}

	write(`telegram:
  token: "test-token"
  allowed_users:
    - 1
logging:
  level: INFO
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := cfg.Telegram.DeleteAfterDuration()
	if err != nil {
		t.Fatal(err)
	}
	if d != DefaultDeleteAfter {
		t.Fatalf("loaded default = %v", d)
	}
}
