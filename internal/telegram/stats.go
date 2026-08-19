package telegram

import (
	"sync"
	"time"
)

type connStats struct {
	mu            sync.Mutex
	startedAt     time.Time
	totalFailures int
	failures      []time.Time
}

func newConnStats() *connStats {
	return &connStats{startedAt: time.Now()}
}

func (s *connStats) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.totalFailures++
	s.failures = append(s.failures, now)
	s.pruneLocked(now.Add(-24 * time.Hour))
}

func (s *connStats) pruneLocked(cutoff time.Time) {
	i := 0
	for _, t := range s.failures {
		if t.After(cutoff) {
			s.failures[i] = t
			i++
		}
	}
	s.failures = s.failures[:i]
}

func (s *connStats) counts() (hour, day, total int, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneLocked(now.Add(-24 * time.Hour))
	hourCutoff := now.Add(-time.Hour)
	for _, t := range s.failures {
		if t.After(hourCutoff) {
			hour++
		}
	}
	return hour, len(s.failures), s.totalFailures, s.startedAt
}
