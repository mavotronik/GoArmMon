package state

import "testing"

func TestCheckSnapshotStatusLabel(t *testing.T) {
	snap := CheckSnapshot{
		Status:            StatusPartial,
		PingFailThreshold: 3,
		PingFails:         2,
	}
	if got := snap.StatusLabel(); got != "PARTIAL (3/2)" {
		t.Fatalf("expected PARTIAL (3/2), got %q", got)
	}
}

func TestHostViewStatusLabel(t *testing.T) {
	host := HostView{
		Status: StatusPartial,
		Checks: []CheckSnapshot{{
			Status:            StatusPartial,
			PingFailThreshold: 5,
			PingFails:         1,
		}},
	}
	if got := host.StatusLabel(); got != "PARTIAL (5/1)" {
		t.Fatalf("expected PARTIAL (5/1), got %q", got)
	}
}
