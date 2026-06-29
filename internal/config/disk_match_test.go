package config

import "testing"

func TestIsDiskIgnored(t *testing.T) {
	disk := &DiskThreshold{
		IgnoreDevices: []string{"/dev/loop0", "zram*"},
		IgnoreMounts:  []string{"/boot", "/snap/*"},
	}

	tests := []struct {
		device string
		mount  string
		want   bool
	}{
		{"/dev/loop0", "/", true},
		{"loop0", "/", true},
		{"/dev/zram0", "/", true},
		{"/dev/sda1", "/", false},
		{"/dev/sda1", "/boot", true},
		{"/dev/sda1", "/snap/firefox", true},
		{"/dev/sda1", "/", false},
	}

	for _, tt := range tests {
		if got := IsDiskIgnored(tt.device, tt.mount, disk); got != tt.want {
			t.Fatalf("IsDiskIgnored(%q, %q) = %v, want %v", tt.device, tt.mount, got, tt.want)
		}
	}
}

func TestMatchDiskPattern(t *testing.T) {
	if !matchDiskPattern("loop0", "loop*") {
		t.Fatal("expected glob match for device basename")
	}
	if matchDiskPattern("/dev/sda1", "loop*") {
		t.Fatal("unexpected glob match")
	}
}
