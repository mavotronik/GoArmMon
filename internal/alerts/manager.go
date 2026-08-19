package alerts

import (
	"fmt"
	"sync"
	"time"

	"goarmmon/internal/config"
	"goarmmon/internal/state"
)

type EventKind string

const (
	EventOffline   EventKind = "offline"
	EventOnline    EventKind = "online"
	EventWarning   EventKind = "warning"
	EventCritical  EventKind = "critical"
	EventRecovery  EventKind = "recovery"
	EventPartial   EventKind = "partial"
)

type Event struct {
	Kind      EventKind
	HostName  string
	CheckType string
	CheckID   string
	OldStatus state.Status
	NewStatus state.Status
	Message   string
	At        time.Time
}

type metricLevel int

const (
	levelNone metricLevel = iota
	levelWarning
	levelCritical
)

type metricState struct {
	level       metricLevel
	breachSince time.Time
}

type availabilityState struct {
	failStreak int
}

type Manager struct {
	mu             sync.Mutex
	hosts          map[string]config.HostConfig
	checks         map[string]state.Status
	metrics        map[string]metricState
	availability   map[string]availabilityState
	partialLabels  map[string]string
	events         chan Event
	now            func() time.Time
}

func NewManager(hosts []config.HostConfig, buffer int) *Manager {
	if buffer <= 0 {
		buffer = 256
	}
	m := &Manager{
		hosts:         make(map[string]config.HostConfig),
		checks:        make(map[string]state.Status),
		metrics:       make(map[string]metricState),
		availability:  make(map[string]availabilityState),
		partialLabels: make(map[string]string),
		events:        make(chan Event, buffer),
		now:           time.Now,
	}
	m.UpdateHosts(hosts)
	return m
}

func (m *Manager) UpdateHosts(hosts []config.HostConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]config.HostConfig, len(hosts))
	for _, h := range hosts {
		next[h.Name] = h
	}
	m.hosts = next
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Evaluate(snap state.CheckSnapshot) state.CheckSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	host, ok := m.hosts[snap.HostName]
	if !ok {
		return snap
	}

	oldStatus, seen := m.checks[snap.CheckID]
	if !seen {
		oldStatus = state.StatusUnknown
	}

	effective := snap.Status
	if snap.Skipped {
		effective = oldStatus
		if effective == state.StatusUnknown {
			effective = state.StatusOnline
		}
		m.checks[snap.CheckID] = effective
		snap.Status = effective
		return snap
	}

	switch snap.CheckType {
	case "ping":
		effective = m.evalPingAvailability(host, &snap, oldStatus)
		effective = m.evalRTT(host, snap, effective)
	case "http":
		effective = m.evalAvailability(snap, oldStatus)
		effective = m.evalHTTPResponse(host, snap, effective)
	case "glances":
		if snap.Status == state.StatusOffline {
			effective = state.StatusOffline
		} else {
			effective = m.evalGlances(host, snap)
		}
	default:
		effective = m.evalAvailability(snap, oldStatus)
	}

	if effective != oldStatus {
		m.emitTransition(snap, oldStatus, effective)
	}

	newPartialLabel := state.PartialLabel(snap.PingFailThreshold, snap.PingFails)
	oldPartialLabel := m.partialLabels[snap.CheckID]
	if newPartialLabel != oldPartialLabel {
		if effective != state.StatusOffline {
			m.emitPartialTransition(snap, oldPartialLabel, newPartialLabel)
		}
		m.partialLabels[snap.CheckID] = newPartialLabel
	}

	m.checks[snap.CheckID] = effective
	snap.Status = effective
	return snap
}

func (m *Manager) evalAvailability(snap state.CheckSnapshot, old state.Status) state.Status {
	if snap.Status == state.StatusOffline {
		return state.StatusOffline
	}
	if snap.Status == state.StatusOnline {
		return state.StatusOnline
	}
	return old
}

func (m *Manager) evalPingAvailability(host config.HostConfig, snap *state.CheckSnapshot, old state.Status) state.Status {
	key := snap.CheckID + ":avail"
	if snap.Status == state.StatusOnline {
		m.setAvailabilityState(key, availabilityState{})
		snap.PingFails = 0
		snap.PingFailThreshold = 0
		return state.StatusOnline
	}
	if snap.Status != state.StatusOffline {
		return old
	}

	st := m.availabilityState(key)
	st.failStreak++
	m.setAvailabilityState(key, st)

	threshold := host.PingFailThreshold
	if threshold <= 0 {
		threshold = 1
	}
	if st.failStreak >= threshold {
		snap.PingFails = 0
		snap.PingFailThreshold = 0
		return state.StatusOffline
	}
	snap.PingFails = st.failStreak
	snap.PingFailThreshold = threshold
	return state.StatusPartial
}

func (m *Manager) evalRTT(host config.HostConfig, snap state.CheckSnapshot, current state.Status) state.Status {
	if host.Alerts.RTT == nil || !host.Alerts.RTT.Enabled() {
		return current
	}
	if current == state.StatusOffline {
		return state.StatusOffline
	}
	value := float64(snap.RTT.Milliseconds())
	return m.evalMetric(snap.CheckID, "rtt", host.Alerts.RTT, value, current, host.Alerts.For)
}

func (m *Manager) evalHTTPResponse(host config.HostConfig, snap state.CheckSnapshot, current state.Status) state.Status {
	if host.Alerts.HTTPResponse == nil || !host.Alerts.HTTPResponse.Enabled() {
		return current
	}
	if snap.Status == state.StatusOffline {
		return state.StatusOffline
	}
	value := float64(snap.HTTPDuration.Milliseconds())
	return m.evalMetric(snap.CheckID, "http_response", host.Alerts.HTTPResponse, value, current, host.Alerts.For)
}

func (m *Manager) evalGlances(host config.HostConfig, snap state.CheckSnapshot) state.Status {
	if snap.Glances == nil {
		return state.StatusOnline
	}
	st := state.StatusOnline
	g := snap.Glances

	if host.Alerts.CPU != nil && host.Alerts.CPU.Enabled() {
		st = state.WorstStatus(st, metricToStatus(m.evalMetricLevel(snap.CheckID, "cpu", host.Alerts.CPU, g.CPU, host.Alerts.For)))
	}
	if host.Alerts.RAM != nil && host.Alerts.RAM.Enabled() {
		st = state.WorstStatus(st, metricToStatus(m.evalMetricLevel(snap.CheckID, "ram", host.Alerts.RAM, g.RAM, host.Alerts.For)))
	}
	if host.Alerts.Swap != nil && host.Alerts.Swap.Enabled() {
		st = state.WorstStatus(st, metricToStatus(m.evalMetricLevel(snap.CheckID, "swap", host.Alerts.Swap, g.Swap, host.Alerts.For)))
	}
	if host.Alerts.Disk != nil && host.Alerts.Disk.Enabled() {
		st = state.WorstStatus(st, metricToStatus(m.evalMetricLevel(snap.CheckID, "disk", &host.Alerts.Disk.Threshold, g.MaxDiskPct, host.Alerts.For)))
	}
	return st
}

func metricToStatus(level metricLevel) state.Status {
	switch level {
	case levelCritical:
		return state.StatusCritical
	case levelWarning:
		return state.StatusWarning
	default:
		return state.StatusOnline
	}
}

func (m *Manager) evalMetric(checkID, name string, th *config.Threshold, value float64, current state.Status, defaultFor *string) state.Status {
	level := m.evalMetricLevel(checkID, name, th, value, defaultFor)
	st := metricToStatus(level)
	if current == state.StatusOffline {
		return state.StatusOffline
	}
	return state.WorstStatus(state.StatusOnline, st)
}

func (m *Manager) evalMetricLevel(checkID, name string, th *config.Threshold, value float64, defaultFor *string) metricLevel {
	key := checkID + ":" + name
	prev := m.metricState(key)

	critical := th.Critical != nil && value >= *th.Critical
	warning := th.Warning != nil && value >= *th.Warning

	switch prev.level {
	case levelCritical:
		recovery := th.RecoveryForCritical()
		if value <= recovery {
			if th.Warning != nil && value >= *th.Warning {
				m.setMetricState(key, metricState{level: levelWarning})
				return levelWarning
			}
			m.setMetricState(key, metricState{})
			return levelNone
		}
		return levelCritical
	case levelWarning:
		if critical {
			m.setMetricState(key, metricState{level: levelCritical})
			return levelCritical
		}
		recovery := th.RecoveryForWarning()
		if value <= recovery {
			m.setMetricState(key, metricState{})
			return levelNone
		}
		return levelWarning
	default:
		if !warning && !critical {
			m.setMetricState(key, metricState{})
			return levelNone
		}
		return m.applyForbearance(key, th, defaultFor, critical, warning)
	}
}

func (m *Manager) applyForbearance(key string, th *config.Threshold, defaultFor *string, critical, warning bool) metricLevel {
	forbearance, err := th.Forbearance(defaultFor)
	if err != nil || forbearance == 0 {
		if critical {
			m.setMetricState(key, metricState{level: levelCritical})
			return levelCritical
		}
		m.setMetricState(key, metricState{level: levelWarning})
		return levelWarning
	}

	prev := m.metricState(key)
	now := m.now()
	if prev.breachSince.IsZero() {
		m.setMetricState(key, metricState{breachSince: now})
		return levelNone
	}
	if now.Sub(prev.breachSince) < forbearance {
		return levelNone
	}
	if critical {
		m.setMetricState(key, metricState{level: levelCritical})
		return levelCritical
	}
	m.setMetricState(key, metricState{level: levelWarning})
	return levelWarning
}

func (m *Manager) metricState(key string) metricState {
	return m.metrics[key]
}

func (m *Manager) setMetricState(key string, st metricState) {
	m.metrics[key] = st
}

func (m *Manager) availabilityState(key string) availabilityState {
	return m.availability[key]
}

func (m *Manager) setAvailabilityState(key string, st availabilityState) {
	m.availability[key] = st
}

func (m *Manager) emitPartialTransition(snap state.CheckSnapshot, oldLabel, newLabel string) {
	if oldLabel == newLabel {
		return
	}
	msg := formatPartialMessage(snap, oldLabel, newLabel)
	ev := Event{
		Kind:      EventPartial,
		HostName:  snap.HostName,
		CheckType: snap.CheckType,
		CheckID:   snap.CheckID,
		OldStatus: snap.Status,
		NewStatus: snap.Status,
		Message:   msg,
		At:        time.Now(),
	}
	select {
	case m.events <- ev:
	default:
	}
}

func formatPartialMessage(snap state.CheckSnapshot, oldLabel, newLabel string) string {
	switch {
	case newLabel == "":
		return fmt.Sprintf("%s/%s: PARTIAL (%s) -> ONLINE", snap.HostName, snap.CheckType, oldLabel)
	case oldLabel == "":
		return fmt.Sprintf("%s/%s: ONLINE -> PARTIAL (%s)", snap.HostName, snap.CheckType, newLabel)
	default:
		return fmt.Sprintf("%s/%s: PARTIAL (%s) -> PARTIAL (%s)", snap.HostName, snap.CheckType, oldLabel, newLabel)
	}
}

func (m *Manager) emitTransition(snap state.CheckSnapshot, oldStatus, newStatus state.Status) {
	kind := classifyTransition(oldStatus, newStatus)
	if kind == "" {
		return
	}
	ev := Event{
		Kind:      kind,
		HostName:  snap.HostName,
		CheckType: snap.CheckType,
		CheckID:   snap.CheckID,
		OldStatus: oldStatus,
		NewStatus: newStatus,
		Message:   formatMessage(snap, oldStatus, newStatus),
		At:        time.Now(),
	}
	select {
	case m.events <- ev:
	default:
	}
}

func classifyTransition(old, new state.Status) EventKind {
	if old == new {
		return ""
	}
	if old == state.StatusUnknown && new == state.StatusOnline {
		return ""
	}
	if new == state.StatusPartial {
		return ""
	}
	if old == state.StatusPartial && new == state.StatusOnline {
		return ""
	}
	if new == state.StatusOffline {
		return EventOffline
	}
	if old == state.StatusOffline && (new == state.StatusOnline || new == state.StatusWarning) {
		return EventOnline
	}
	if new == state.StatusCritical {
		return EventCritical
	}
	if new == state.StatusWarning {
		return EventWarning
	}
	if (old == state.StatusCritical || old == state.StatusWarning) && new == state.StatusOnline {
		return EventRecovery
	}
	return EventRecovery
}

func formatMessage(snap state.CheckSnapshot, old, new state.Status) string {
	base := fmt.Sprintf("%s/%s: %s -> %s", snap.HostName, snap.CheckType, old, new)
	if snap.Error != "" {
		return base + ": " + snap.Error
	}
	switch snap.CheckType {
	case "ping":
		return fmt.Sprintf("%s (RTT %s)", base, snap.RTT.Round(time.Millisecond))
	case "http":
		return fmt.Sprintf("%s (code %d, %s)", base, snap.HTTPCode, snap.HTTPDuration.Round(time.Millisecond))
	case "glances":
		if snap.Glances != nil {
			return fmt.Sprintf("%s (CPU %.1f%%, RAM %.1f%%)", base, snap.Glances.CPU, snap.Glances.RAM)
		}
	}
	return base
}
