package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"goarmmon/internal/alerts"
	"goarmmon/internal/checks"
	"goarmmon/internal/config"
	"goarmmon/internal/state"

	_ "goarmmon/internal/checks/glances"
	_ "goarmmon/internal/checks/http"
	_ "goarmmon/internal/checks/ping"
)

type Scheduler struct {
	cache      *state.Cache
	alerts     *alerts.Manager
	mu         sync.Mutex
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	hostCfg    map[string]config.HostConfig
	logResults bool
}

func New(cache *state.Cache, alertMgr *alerts.Manager) *Scheduler {
	return &Scheduler{
		cache:   cache,
		alerts:  alertMgr,
		hostCfg: make(map[string]config.HostConfig),
	}
}

func (s *Scheduler) ApplyConfig(ctx context.Context, cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	hostCfg := make(map[string]config.HostConfig, len(cfg.Hosts))
	hostMeta := make([]state.HostMeta, 0, len(cfg.Hosts))
	for _, h := range cfg.Hosts {
		hostCfg[h.Name] = h
		hostMeta = append(hostMeta, state.HostMeta{
			Name:        h.Name,
			Description: h.Description,
			Group:       h.Group,
		})
	}
	s.hostCfg = hostCfg
	s.logResults = cfg.Logging.LogResults
	s.cache.SetHosts(hostMeta)
	s.alerts.UpdateHosts(cfg.Hosts)

	for _, host := range cfg.Hosts {
		runners, err := checks.BuildAll(host)
		if err != nil {
			slog.Error("build checks failed", "host", host.Name, "error", err)
			continue
		}
		for _, runner := range runners {
			s.wg.Add(1)
			go s.runCheck(runCtx, host, runner)
		}
	}
}

func (s *Scheduler) runCheck(ctx context.Context, host config.HostConfig, runner checks.Runner) {
	defer s.wg.Done()

	interval := runner.Interval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runOnce := func() {
		if host.SkipOnPingFailure && runner.Type() != "ping" {
			if st, ok := s.cache.PingStatus(host.Name); ok && st == state.StatusOffline {
				snap := state.CheckSnapshot{
					HostName:  host.Name,
					CheckType: runner.Type(),
					CheckID:   runner.ID(),
					Skipped:   true,
					UpdatedAt: time.Now(),
				}
				if prev, ok := s.cache.GetCheck(runner.ID()); ok {
					snap.Status = prev.Status
				} else {
					snap.Status = state.StatusUnknown
				}
				snap = s.alerts.Evaluate(snap)
				s.cache.Update(snap)
				s.logCheckResult(snap)
				return
			}
		}

		snap := runner.Run(ctx)
		snap = s.alerts.Evaluate(snap)
		s.cache.Update(snap)
		s.logCheckResult(snap)
	}

	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func (s *Scheduler) logCheckResult(snap state.CheckSnapshot) {
	if !s.logResults {
		return
	}

	attrs := []any{
		"host", snap.HostName,
		"type", snap.CheckType,
		"status", snap.Status,
	}
	if snap.Skipped {
		attrs = append(attrs, "skipped", true)
		slog.Info("check result", attrs...)
		return
	}
	if snap.Error != "" {
		attrs = append(attrs, "error", snap.Error)
	}

	switch snap.CheckType {
	case "ping":
		attrs = append(attrs, "rtt", snap.RTT)
		if snap.PingFailThreshold > 0 {
			attrs = append(attrs, "fails", snap.PingFails, "fail_threshold", snap.PingFailThreshold)
		}
	case "http":
		attrs = append(attrs, "code", snap.HTTPCode, "duration", snap.HTTPDuration)
	case "glances":
		if snap.Glances != nil {
			attrs = append(attrs,
				"cpu", snap.Glances.CPU,
				"ram", snap.Glances.RAM,
				"swap", snap.Glances.Swap,
			)
		}
	}

	slog.Info("check result", attrs...)
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
		s.cancel = nil
	}
}
