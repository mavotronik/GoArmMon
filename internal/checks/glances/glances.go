package glances

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"goarmmon/internal/checks"
	"goarmmon/internal/config"
	"goarmmon/internal/state"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

func init() {
	checks.Register("glances", New)
}

type checkConfig struct {
	URL        string `yaml:"url"`
	APIVersion string `yaml:"api_version"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Token      string `yaml:"token"`
	Timeout    string `yaml:"timeout"`
	Interval   string `yaml:"interval"`
}

type Runner struct {
	host     config.HostConfig
	baseURL  string
	version  string
	username string
	password string
	token    string
	interval time.Duration
	client   *http.Client
}

func New(host config.HostConfig, raw yaml.Node) (checks.Runner, error) {
	var cfg checkConfig
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("glances.url is required")
	}
	timeout, err := config.ParseDuration(cfg.Timeout, "glances.timeout")
	if err != nil {
		return nil, err
	}
	interval, err := config.ParseDuration(cfg.Interval, "glances.interval")
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	version := strings.ToLower(strings.TrimSpace(cfg.APIVersion))
	if version == "" || version == "auto" {
		version = ""
	}

	client := &http.Client{Timeout: timeout}
	r := &Runner{
		host:     host,
		baseURL:  baseURL,
		version:  version,
		username: cfg.Username,
		password: cfg.Password,
		token:    cfg.Token,
		interval: interval,
		client:   client,
	}

	if r.version == "" {
		v, err := r.detectVersion(context.Background())
		if err != nil {
			return nil, err
		}
		r.version = v
	}

	return r, nil
}

func (r *Runner) ID() string       { return checks.CheckID(r.host.Name, "glances") }
func (r *Runner) Type() string     { return "glances" }
func (r *Runner) HostName() string { return r.host.Name }
func (r *Runner) Interval() time.Duration {
	return r.interval
}

func (r *Runner) Run(ctx context.Context) state.CheckSnapshot {
	snap := state.CheckSnapshot{
		HostName:  r.host.Name,
		CheckType: "glances",
		CheckID:   r.ID(),
		Status:    state.StatusUnknown,
		UpdatedAt: time.Now(),
	}

	data := &state.GlancesData{}
	var mu sync.Mutex
	var firstErr error

	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var v struct {
			Total float64 `json:"total"`
		}
		if err := r.fetchJSON(gctx, "cpu", &v); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		data.CPU = v.Total
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var v struct {
			Percent float64 `json:"percent"`
		}
		if err := r.fetchJSON(gctx, "mem", &v); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		data.RAM = v.Percent
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var v struct {
			Percent float64 `json:"percent"`
		}
		if err := r.fetchJSON(gctx, "memswap", &v); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		data.Swap = v.Percent
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var v struct {
			Min1  float64 `json:"min1"`
			Min5  float64 `json:"min5"`
			Min15 float64 `json:"min15"`
		}
		if err := r.fetchJSON(gctx, "load", &v); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		data.Load = [3]float64{v.Min1, v.Min5, v.Min15}
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var raw json.RawMessage
		if err := r.fetchRaw(gctx, "uptime", &raw); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		var seconds float64
		if err := json.Unmarshal(raw, &seconds); err == nil {
			data.Uptime = time.Duration(seconds * float64(time.Second))
			return nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if d, err := time.ParseDuration(strings.TrimSpace(s) + "s"); err == nil {
				data.Uptime = d
			}
		}
		return nil
	})

	g.Go(func() error {
		var fs []struct {
			DeviceName string  `json:"device_name"`
			MntPoint   string  `json:"mnt_point"`
			Percent    float64 `json:"percent"`
		}
		if err := r.fetchJSON(gctx, "fs", &fs); err != nil {
			setErr(err)
			return nil
		}
		mu.Lock()
		for _, f := range fs {
			ignored := config.IsDiskIgnored(f.DeviceName, f.MntPoint, r.host.Alerts.Disk)
			data.Filesystems = append(data.Filesystems, state.FSInfo{
				Device:  f.DeviceName,
				Mount:   f.MntPoint,
				UsedPct: f.Percent,
				Ignored: ignored,
			})
			if !ignored && f.Percent > data.MaxDiskPct {
				data.MaxDiskPct = f.Percent
			}
		}
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var nets []struct {
			InterfaceName string `json:"interface_name"`
			Rx            uint64 `json:"rx"`
			Tx            uint64 `json:"tx"`
		}
		if err := r.fetchJSON(gctx, "network", &nets); err != nil {
			return nil
		}
		mu.Lock()
		for _, n := range nets {
			data.Network = append(data.Network, state.NetInfo{
				Interface: n.InterfaceName,
				RxBytes:   n.Rx,
				TxBytes:   n.Tx,
			})
		}
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var sensors []struct {
			Label       string  `json:"label"`
			Value       float64 `json:"value"`
			Type        string  `json:"type"`
			Unit        string  `json:"unit"`
		}
		if err := r.fetchJSON(gctx, "sensors", &sensors); err != nil {
			return nil
		}
		mu.Lock()
		for _, s := range sensors {
			if strings.Contains(strings.ToLower(s.Type), "temp") || strings.Contains(strings.ToLower(s.Unit), "c") {
				data.Temperatures = append(data.Temperatures, state.SensorInfo{
					Label:       s.Label,
					Temperature: s.Value,
				})
			}
		}
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		var v struct {
			Total int `json:"total"`
		}
		if err := r.fetchJSON(gctx, "processcount", &v); err != nil {
			return nil
		}
		mu.Lock()
		data.ProcessCount = v.Total
		mu.Unlock()
		return nil
	})

	_ = g.Wait()

	if firstErr != nil {
		snap.Error = firstErr.Error()
		snap.Status = state.StatusOffline
		return snap
	}

	snap.Glances = data
	snap.Status = state.StatusOnline
	return snap
}

func (r *Runner) detectVersion(ctx context.Context) (string, error) {
	for _, v := range []string{"4", "3"} {
		req, err := r.authRequest(ctx, fmt.Sprintf("%s/api/%s/pluginslist", r.baseURL, v))
		if err != nil {
			return "", err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return v, nil
		}
	}
	return "", fmt.Errorf("unable to detect glances API version at %s", r.baseURL)
}

func (r *Runner) fetchJSON(ctx context.Context, plugin string, dest any) error {
	return r.fetchRaw(ctx, plugin, dest)
}

func (r *Runner) authRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	} else if r.username != "" || r.password != "" {
		req.SetBasicAuth(r.username, r.password)
	}
	return req, nil
}

func (r *Runner) fetchRaw(ctx context.Context, plugin string, dest any) error {
	url := fmt.Sprintf("%s/api/%s/%s", r.baseURL, r.version, plugin)
	req, err := r.authRequest(ctx, url)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s: status %d: %s", plugin, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("%s: decode: %w", plugin, err)
	}
	return nil
}
