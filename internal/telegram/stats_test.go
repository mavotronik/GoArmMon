package telegram

import (
	"testing"
	"time"
)

func TestConnStatsCounts(t *testing.T) {
	stats := newConnStats()
	stats.startedAt = time.Now().Add(-2 * time.Hour)

	now := time.Now()
	stats.mu.Lock()
	stats.failures = []time.Time{
		now.Add(-30 * time.Minute),
		now.Add(-90 * time.Minute),
		now.Add(-25 * time.Hour),
	}
	stats.totalFailures = 3
	stats.mu.Unlock()

	hour, day, total, _ := stats.counts()
	if hour != 1 {
		t.Fatalf("hour = %d, want 1", hour)
	}
	if day != 2 {
		t.Fatalf("day = %d, want 2", day)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
}

func TestConnStatsRecordFailure(t *testing.T) {
	stats := newConnStats()
	stats.recordFailure()
	stats.recordFailure()

	_, _, total, _ := stats.counts()
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
}
