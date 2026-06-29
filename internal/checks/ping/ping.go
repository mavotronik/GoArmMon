package ping

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goarmmon/internal/checks"
	"goarmmon/internal/config"
	"goarmmon/internal/state"

	ping "github.com/go-ping/ping"
	"gopkg.in/yaml.v3"
)

func init() {
	checks.Register("ping", New)
}

type pingConfig struct {
	Address  string `yaml:"address"`
	Timeout  string `yaml:"timeout"`
	Interval string `yaml:"interval"`
}

type Runner struct {
	host     config.HostConfig
	address  string
	timeout  time.Duration
	interval time.Duration
	useExec  bool
}

func New(host config.HostConfig, raw yaml.Node) (checks.Runner, error) {
	var cfg pingConfig
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("ping.address is required")
	}
	timeout, err := config.ParseDuration(cfg.Timeout, "ping.timeout")
	if err != nil {
		return nil, err
	}
	interval, err := config.ParseDuration(cfg.Interval, "ping.interval")
	if err != nil {
		return nil, err
	}

	r := &Runner{
		host:     host,
		address:  cfg.Address,
		timeout:  timeout,
		interval: interval,
	}

	if !r.probeUnprivileged() {
		r.useExec = true
	}

	return r, nil
}

func (r *Runner) probeUnprivileged() bool {
	p, err := ping.NewPinger(r.address)
	if err != nil {
		return false
	}
	p.SetPrivileged(false)
	p.Count = 1
	p.Timeout = r.timeout
	if err := p.Run(); err != nil {
		return false
	}
	return p.Statistics().PacketsRecv > 0
}

func (r *Runner) ID() string       { return checks.CheckID(r.host.Name, "ping") }
func (r *Runner) Type() string     { return "ping" }
func (r *Runner) HostName() string { return r.host.Name }
func (r *Runner) Interval() time.Duration {
	return r.interval
}

func (r *Runner) Run(ctx context.Context) state.CheckSnapshot {
	snap := state.CheckSnapshot{
		HostName:  r.host.Name,
		CheckType: "ping",
		CheckID:   r.ID(),
		Status:    state.StatusUnknown,
		UpdatedAt: time.Now(),
	}

	var online bool
	var rtt time.Duration
	var err error

	if r.useExec {
		online, rtt, err = r.runExec(ctx)
	} else {
		online, rtt, err = r.runUnprivileged(ctx)
	}

	if err != nil {
		snap.Error = err.Error()
		snap.Status = state.StatusOffline
		return snap
	}

	snap.RTT = rtt
	if online {
		snap.Status = state.StatusOnline
	} else {
		snap.Status = state.StatusOffline
	}
	return snap
}

func (r *Runner) runUnprivileged(ctx context.Context) (bool, time.Duration, error) {
	p, err := ping.NewPinger(r.address)
	if err != nil {
		return false, 0, err
	}
	p.SetPrivileged(false)
	p.Count = 1
	p.Timeout = r.timeout

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.Stop()
		case <-done:
		}
	}()

	if err := p.Run(); err != nil {
		close(done)
		return false, 0, err
	}
	close(done)

	stats := p.Statistics()
	if stats.PacketsRecv == 0 {
		return false, 0, nil
	}
	return true, stats.AvgRtt, nil
}

var execRTT = regexp.MustCompile(`time[=<]([\d.]+)\s*ms`)

func (r *Runner) runExec(ctx context.Context) (bool, time.Duration, error) {
	waitSec := int(r.timeout.Seconds())
	if waitSec < 1 {
		waitSec = 1
	}
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(waitSec), r.address)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, 0, nil
	}
	text := string(out)
	if !strings.Contains(text, "1 received") && !strings.Contains(text, "1 packets received") {
		return false, 0, nil
	}
	m := execRTT.FindStringSubmatch(text)
	if len(m) < 2 {
		return true, 0, nil
	}
	ms, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return true, 0, nil
	}
	return true, time.Duration(ms * float64(time.Millisecond)), nil
}
