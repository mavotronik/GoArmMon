package config

import "testing"

func TestCheckHelpers(t *testing.T) {
	h := DefaultHost("test")

	if !HasCheck(h, "ping") {
		t.Fatal("default host should have ping")
	}
	if HasCheck(h, "http") {
		t.Fatal("default host should not have http yet")
	}

	if err := SetHTTPCheck(&h, DefaultHTTPCheck()); err != nil {
		t.Fatal(err)
	}
	httpCfg, _, ok := GetHTTPCheck(&h)
	if !ok || httpCfg.Method != "GET" {
		t.Fatalf("http check: ok=%v cfg=%+v", ok, httpCfg)
	}

	types := ListCheckTypes(h)
	if len(types) != 2 {
		t.Fatalf("check types = %v, want [ping http]", types)
	}

	if !RemoveCheck(&h, "http") {
		t.Fatal("expected http removed")
	}
	if len(h.Checks) != 1 {
		t.Fatalf("checks len = %d", len(h.Checks))
	}

	ping, _, ok := GetPingCheck(&h)
	if !ok {
		t.Fatal("ping should remain")
	}
	ping.Address = "10.0.0.1"
	if err := SetPingCheck(&h, *ping); err != nil {
		t.Fatal(err)
	}
	ping2, _, ok := GetPingCheck(&h)
	if !ok || ping2.Address != "10.0.0.1" {
		t.Fatalf("ping address not updated: %+v", ping2)
	}
}

func TestGlancesCheckRoundTrip(t *testing.T) {
	h := HostConfig{Name: "srv"}
	cfg := DefaultGlancesCheck()
	cfg.URL = "http://127.0.0.1:61208"
	if err := SetGlancesCheck(&h, cfg); err != nil {
		t.Fatal(err)
	}
	got, _, ok := GetGlancesCheck(&h)
	if !ok || got.URL != cfg.URL || got.APIVersion != "auto" {
		t.Fatalf("glances round-trip failed: ok=%v got=%+v", ok, got)
	}
}

func TestDiskAlertLists(t *testing.T) {
	h := HostConfig{Name: "srv"}
	h.Alerts.Disk = &DiskThreshold{
		IgnoreDevices: []string{"loop*"},
		IgnoreMounts:  []string{"/boot"},
	}
	w := 90.0
	h.Alerts.Disk.Critical = &w

	clone, err := Clone(&Config{
		Telegram: TelegramConfig{Token: "t", AllowedUsers: []int64{1}},
		Hosts:    []HostConfig{h},
	})
	if err != nil {
		t.Fatal(err)
	}
	disk := clone.Hosts[0].Alerts.Disk
	if disk == nil || len(disk.IgnoreDevices) != 1 || disk.IgnoreDevices[0] != "loop*" {
		t.Fatalf("disk ignore lists not preserved: %+v", disk)
	}
}
