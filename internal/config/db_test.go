package config

import (
	"path/filepath"
	"testing"
)

func TestResolveDBDirDefault(t *testing.T) {
	got := ResolveDBDir("/etc/goarmmon/config.yaml", "")
	want := filepath.Join("/etc/goarmmon", "db")
	if got != want {
		t.Fatalf("ResolveDBDir() = %q, want %q", got, want)
	}
}

func TestResolveDBDirRelative(t *testing.T) {
	got := ResolveDBDir("/etc/goarmmon/config.yaml", "data")
	want := filepath.Join("/etc/goarmmon", "data")
	if got != want {
		t.Fatalf("ResolveDBDir() = %q, want %q", got, want)
	}
}

func TestResolveDBDirAbsolute(t *testing.T) {
	got := ResolveDBDir("/etc/goarmmon/config.yaml", "/var/lib/goarmmon")
	if got != "/var/lib/goarmmon" {
		t.Fatalf("ResolveDBDir() = %q, want absolute path", got)
	}
}

func TestACLDBFile(t *testing.T) {
	got := ACLDBFile("/var/lib/goarmmon/db")
	want := filepath.Join("/var/lib/goarmmon/db", "acl.sqlite")
	if got != want {
		t.Fatalf("ACLDBFile() = %q, want %q", got, want)
	}
}

func TestPrimaryRoot(t *testing.T) {
	cfg := TelegramConfig{AllowedUsers: []int64{11, 22}}
	if cfg.PrimaryRoot() != 11 {
		t.Fatalf("PrimaryRoot() = %d, want 11", cfg.PrimaryRoot())
	}
	if (TelegramConfig{}).PrimaryRoot() != 0 {
		t.Fatal("empty AllowedUsers should return 0")
	}
}
