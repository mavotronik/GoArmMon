package hoststore

import (
	"path/filepath"
	"testing"

	"goarmmon/internal/config"
)

func sampleHost(name, addr string) config.HostConfig {
	h := config.DefaultHost(name)
	ping, _, _ := config.GetPingCheck(&h)
	ping.Address = addr
	_ = config.SetPingCheck(&h, *ping)
	return h
}

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "acl.sqlite")
	store, err := Open(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dbFile
}

func TestReplaceAllAndListRoundTrip(t *testing.T) {
	store, _ := openTestStore(t)

	hosts := []config.HostConfig{
		sampleHost("router", "192.168.1.1"),
		sampleHost("server", "192.168.1.69"),
	}
	hosts[0].Description = "Home router"
	hosts[0].Messages = config.HostMessages{Offline: "down", Online: "up"}
	hosts[1].Alerts.For = strPtr("2m")

	if err := store.ReplaceAll(hosts); err != nil {
		t.Fatal(err)
	}

	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "router" || got[0].Description != "Home router" {
		t.Fatalf("first host = %+v", got[0])
	}
	if got[0].Messages.Offline != "down" || got[0].Messages.Online != "up" {
		t.Fatalf("messages = %+v", got[0].Messages)
	}
	if got[1].Name != "server" {
		t.Fatalf("second host = %+v", got[1])
	}
	if got[1].Alerts.For == nil || *got[1].Alerts.For != "2m" {
		t.Fatalf("alerts.for = %+v", got[1].Alerts.For)
	}

	ping, _, ok := config.GetPingCheck(&got[0])
	if !ok || ping.Address != "192.168.1.1" {
		t.Fatalf("ping check = %+v ok=%v", ping, ok)
	}
	if len(got[0].CheckDefinitions()) == 0 {
		t.Fatal("expected prepared check definitions")
	}
}

func TestImportNewPreservesExisting(t *testing.T) {
	store, _ := openTestStore(t)

	existing := sampleHost("router", "192.168.1.1")
	if err := store.ReplaceAll([]config.HostConfig{existing}); err != nil {
		t.Fatal(err)
	}

	fromConfig := []config.HostConfig{
		sampleHost("router", "10.0.0.1"),
		sampleHost("server", "192.168.1.69"),
	}
	fromConfig[0].Description = "should not overwrite"

	result, err := store.ImportNew(fromConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Imported) != 1 || result.Imported[0] != "server" {
		t.Fatalf("imported = %v, want [server]", result.Imported)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "router" {
		t.Fatalf("skipped = %v, want [router]", result.Skipped)
	}

	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "router" {
		t.Fatalf("first host = %q, want router", got[0].Name)
	}
	ping, _, ok := config.GetPingCheck(&got[0])
	if !ok || ping.Address != "192.168.1.1" {
		t.Fatalf("existing host should not be overwritten: %+v", ping)
	}
	if got[1].Name != "server" {
		t.Fatalf("second host = %q, want server", got[1].Name)
	}
}

func TestReplaceAllEmpty(t *testing.T) {
	store, _ := openTestStore(t)

	if err := store.ReplaceAll([]config.HostConfig{sampleHost("router", "1.1.1.1")}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAll(nil); err != nil {
		t.Fatal(err)
	}

	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func strPtr(s string) *string {
	return &s
}
