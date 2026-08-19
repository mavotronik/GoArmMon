package telegram

import (
	"testing"

	"goarmmon/internal/config"
)

func TestParseOptionalFloat(t *testing.T) {
	v, err := parseOptionalFloat("42.5")
	if err != nil || v == nil || *v != 42.5 {
		t.Fatalf("got %v err=%v", v, err)
	}
	v, err = parseOptionalFloat("clear")
	if err != nil || v != nil {
		t.Fatalf("clear should return nil: %v err=%v", v, err)
	}
	_, err = parseOptionalFloat("not-a-number")
	if err == nil {
		t.Fatal("expected error for invalid float")
	}
}

func TestParseStringList(t *testing.T) {
	list, err := parseStringList("a, b , c")
	if err != nil || len(list) != 3 || list[0] != "a" || list[2] != "c" {
		t.Fatalf("got %v err=%v", list, err)
	}
	list, err = parseStringList("-")
	if err != nil || list != nil {
		t.Fatalf("clear should return nil: %v err=%v", list, err)
	}
}

func TestValidateHostName(t *testing.T) {
	if err := validateHostName(""); err == nil {
		t.Fatal("empty name should fail")
	}
	if err := validateHostName("bad name"); err == nil {
		t.Fatal("whitespace name should fail")
	}
	if err := validateHostName("server-1"); err != nil {
		t.Fatalf("valid name failed: %v", err)
	}
}

func TestIsClearValue(t *testing.T) {
	for _, s := range []string{"-", "clear", "CLEAR", "none", "off"} {
		if !isClearValue(s) {
			t.Fatalf("%q should be clear", s)
		}
	}
	if isClearValue("value") {
		t.Fatal("value should not be clear")
	}
}

func TestParseBoolInput(t *testing.T) {
	for _, s := range []string{"on", "true", "yes", "1"} {
		v, err := parseBoolInput(s)
		if err != nil || !v {
			t.Fatalf("%q: got %v err=%v", s, v, err)
		}
	}
	for _, s := range []string{"off", "false", "no", "0"} {
		v, err := parseBoolInput(s)
		if err != nil || v {
			t.Fatalf("%q: got %v err=%v", s, v, err)
		}
	}
}

func TestApplyAlertField(t *testing.T) {
	h := config.DefaultHost("test")
	if err := applyAlertField(&h, "alerts.cpu.warning", "75"); err != nil {
		t.Fatal(err)
	}
	if h.Alerts.CPU == nil || h.Alerts.CPU.Warning == nil || *h.Alerts.CPU.Warning != 75 {
		t.Fatalf("cpu warning not set: %+v", h.Alerts.CPU)
	}
	if err := applyAlertField(&h, "alerts.cpu.warning", "clear"); err != nil {
		t.Fatal(err)
	}
	if h.Alerts.CPU.Warning != nil {
		t.Fatal("cpu warning should be cleared")
	}
}

func TestApplyPingField(t *testing.T) {
	h := config.DefaultHost("test")
	if err := applyPingField(&h, "checks.ping.address", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	ping, _, ok := config.GetPingCheck(&h)
	if !ok || ping.Address != "10.0.0.2" {
		t.Fatalf("address not updated: %+v ok=%v", ping, ok)
	}
}

func TestApplyDiskIgnoreLists(t *testing.T) {
	h := config.DefaultHost("test")
	if err := applyAlertField(&h, "alerts.disk.ignore_devices", "loop*, zram*"); err != nil {
		t.Fatal(err)
	}
	if h.Alerts.Disk == nil || len(h.Alerts.Disk.IgnoreDevices) != 2 {
		t.Fatalf("ignore_devices not set: %+v", h.Alerts.Disk)
	}
}
