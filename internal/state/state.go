package state

import (
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusUnknown  Status = "UNKNOWN"
	StatusOnline   Status = "ONLINE"
	StatusOffline  Status = "OFFLINE"
	StatusWarning  Status = "WARNING"
	StatusCritical Status = "CRITICAL"
)

var statusRank = map[Status]int{
	StatusUnknown:  0,
	StatusOnline:   1,
	StatusWarning:  2,
	StatusOffline:  3,
	StatusCritical: 4,
}

func WorstStatus(a, b Status) Status {
	if statusRank[a] >= statusRank[b] {
		return a
	}
	return b
}

type FSInfo struct {
	Device  string
	Mount   string
	UsedPct float64
	Ignored bool
}

type NetInfo struct {
	Interface string
	RxBytes   uint64
	TxBytes   uint64
}

type SensorInfo struct {
	Label       string
	Temperature float64
}

type GlancesData struct {
	CPU          float64
	RAM          float64
	Swap         float64
	Load         [3]float64
	Uptime       time.Duration
	Filesystems  []FSInfo
	Network      []NetInfo
	Temperatures []SensorInfo
	ProcessCount int
	MaxDiskPct   float64
}

type CheckSnapshot struct {
	HostName     string
	CheckType    string
	CheckID      string
	Status       Status
	UpdatedAt    time.Time
	Error        string
	RTT          time.Duration
	HTTPCode     int
	HTTPDuration time.Duration
	Glances      *GlancesData
	Skipped      bool
}

type HostView struct {
	Name        string
	Description string
	Group       string
	Status      Status
	Checks      []CheckSnapshot
}

type Cache struct {
	mu      sync.RWMutex
	checks  map[string]CheckSnapshot
	hosts   map[string]HostMeta
}

type HostMeta struct {
	Name        string
	Description string
	Group       string
}

func NewCache() *Cache {
	return &Cache{
		checks: make(map[string]CheckSnapshot),
		hosts:  make(map[string]HostMeta),
	}
}

func (c *Cache) SetHosts(hosts []HostMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[string]HostMeta, len(hosts))
	for _, h := range hosts {
		next[h.Name] = h
	}
	c.hosts = next
}

func (c *Cache) Update(snap CheckSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[snap.CheckID] = snap
}

func (c *Cache) GetCheck(id string) (CheckSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.checks[id]
	return s, ok
}

func (c *Cache) hostChecksLocked(name string) []CheckSnapshot {
	out := make([]CheckSnapshot, 0)
	for _, snap := range c.checks {
		if snap.HostName == name {
			out = append(out, snap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CheckType < out[j].CheckType
	})
	return out
}

func (c *Cache) hostStatusLocked(name string) Status {
	checks := c.hostChecksLocked(name)
	if len(checks) == 0 {
		return StatusUnknown
	}
	st := StatusUnknown
	for _, ch := range checks {
		st = WorstStatus(st, ch.Status)
	}
	return st
}

func (c *Cache) GetHost(name string) (HostView, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	meta, ok := c.hosts[name]
	if !ok {
		return HostView{}, false
	}
	return HostView{
		Name:        meta.Name,
		Description: meta.Description,
		Group:       meta.Group,
		Status:      c.hostStatusLocked(name),
		Checks:      c.hostChecksLocked(name),
	}, true
}

func (c *Cache) ListHosts() []HostView {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.hosts))
	for name := range c.hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]HostView, 0, len(names))
	for _, name := range names {
		meta := c.hosts[name]
		out = append(out, HostView{
			Name:        meta.Name,
			Description: meta.Description,
			Group:       meta.Group,
			Status:      c.hostStatusLocked(name),
			Checks:      c.hostChecksLocked(name),
		})
	}
	return out
}

func (c *Cache) ListAlerts() []CheckSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]CheckSnapshot, 0)
	for _, snap := range c.checks {
		if snap.Status == StatusWarning || snap.Status == StatusCritical || snap.Status == StatusOffline {
			out = append(out, snap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostName == out[j].HostName {
			return out[i].CheckType < out[j].CheckType
		}
		return out[i].HostName < out[j].HostName
	})
	return out
}

func (c *Cache) AllPing() []CheckSnapshot {
	return c.byType("ping")
}

func (c *Cache) AllHTTP() []CheckSnapshot {
	return c.byType("http")
}

func (c *Cache) AllGlances() []CheckSnapshot {
	return c.byType("glances")
}

func (c *Cache) byType(typ string) []CheckSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]CheckSnapshot, 0)
	for _, snap := range c.checks {
		if snap.CheckType == typ {
			out = append(out, snap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].HostName < out[j].HostName
	})
	return out
}

func (c *Cache) PingStatus(host string) (Status, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, snap := range c.checks {
		if snap.HostName == host && snap.CheckType == "ping" {
			return snap.Status, true
		}
	}
	return StatusUnknown, false
}

func (c *Cache) Groups() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	set := make(map[string]struct{})
	for _, h := range c.hosts {
		if h.Group != "" {
			set[h.Group] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

func (c *Cache) HostsByGroup(group string) []HostView {
	all := c.ListHosts()
	if group == "" {
		return all
	}
	out := make([]HostView, 0)
	for _, h := range all {
		if h.Group == group {
			out = append(out, h)
		}
	}
	return out
}
