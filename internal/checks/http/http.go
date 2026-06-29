package httpcheck

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"goarmmon/internal/checks"
	"goarmmon/internal/config"
	"goarmmon/internal/state"

	"gopkg.in/yaml.v3"
)

func init() {
	checks.Register("http", New)
}

type checkConfig struct {
	URL             string `yaml:"url"`
	Method          string `yaml:"method"`
	ExpectedCode    int    `yaml:"expected_code"`
	Timeout         string `yaml:"timeout"`
	Interval        string `yaml:"interval"`
	FollowRedirects bool   `yaml:"follow_redirects"`
}

type Runner struct {
	host         config.HostConfig
	url          string
	method       string
	expectedCode int
	interval     time.Duration
	client       *http.Client
}

func New(host config.HostConfig, raw yaml.Node) (checks.Runner, error) {
	var cfg checkConfig
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("http.url is required")
	}
	if cfg.ExpectedCode == 0 {
		cfg.ExpectedCode = 200
	}
	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, fmt.Errorf("http.method must be GET or HEAD")
	}
	timeout, err := config.ParseDuration(cfg.Timeout, "http.timeout")
	if err != nil {
		return nil, err
	}
	interval, err := config.ParseDuration(cfg.Interval, "http.interval")
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: timeout,
	}
	if !cfg.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return &Runner{
		host:         host,
		url:          cfg.URL,
		method:       method,
		expectedCode: cfg.ExpectedCode,
		interval:     interval,
		client:       client,
	}, nil
}

func (r *Runner) ID() string       { return checks.CheckID(r.host.Name, "http") }
func (r *Runner) Type() string     { return "http" }
func (r *Runner) HostName() string { return r.host.Name }
func (r *Runner) Interval() time.Duration {
	return r.interval
}

func (r *Runner) Run(ctx context.Context) state.CheckSnapshot {
	snap := state.CheckSnapshot{
		HostName:  r.host.Name,
		CheckType: "http",
		CheckID:   r.ID(),
		Status:    state.StatusUnknown,
		UpdatedAt: time.Now(),
	}

	req, err := http.NewRequestWithContext(ctx, r.method, r.url, nil)
	if err != nil {
		snap.Error = err.Error()
		snap.Status = state.StatusOffline
		return snap
	}

	start := time.Now()
	resp, err := r.client.Do(req)
	duration := time.Since(start)
	snap.HTTPDuration = duration

	if err != nil {
		snap.Error = err.Error()
		snap.Status = state.StatusOffline
		return snap
	}
	defer resp.Body.Close()

	snap.HTTPCode = resp.StatusCode
	if resp.StatusCode == r.expectedCode {
		snap.Status = state.StatusOnline
	} else {
		snap.Status = state.StatusOffline
		snap.Error = fmt.Sprintf("expected status %d, got %d", r.expectedCode, resp.StatusCode)
	}
	return snap
}
