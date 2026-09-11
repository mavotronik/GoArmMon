package hostpause

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	Preset5m      = "5m"
	Preset15m     = "15m"
	Preset3h      = "3h"
	Preset12h     = "12h"
	Preset24h     = "24h"
	PresetForever = "forever"
)

var presetDurations = map[string]time.Duration{
	Preset5m:  5 * time.Minute,
	Preset15m: 15 * time.Minute,
	Preset3h:  3 * time.Hour,
	Preset12h: 12 * time.Hour,
	Preset24h: 24 * time.Hour,
}

// Pause describes an active check pause for a host.
type Pause struct {
	Preset string
	Until  *time.Time // nil means forever
}

// Store persists host check pauses in SQLite with an in-memory cache.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
	m  map[string]Pause
}

func Open(dbFile string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.ToSlash(dbFile))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db, m: make(map[string]Pause)}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.loadAll(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS host_check_pauses (
	host_name TEXT PRIMARY KEY,
	until_unix INTEGER,
	preset TEXT NOT NULL
);
`)
	return err
}

func (s *Store) loadAll() error {
	rows, err := s.db.Query(`SELECT host_name, until_unix, preset FROM host_check_pauses`)
	if err != nil {
		return fmt.Errorf("load pauses: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]Pause)

	for rows.Next() {
		var name, preset string
		var untilUnix sql.NullInt64
		if err := rows.Scan(&name, &untilUnix, &preset); err != nil {
			return fmt.Errorf("scan pause: %w", err)
		}
		p := Pause{Preset: preset}
		if untilUnix.Valid {
			t := time.Unix(untilUnix.Int64, 0)
			if t.Before(now) {
				continue
			}
			p.Until = &t
		}
		s.m[name] = p
	}
	return rows.Err()
}

// IsPaused reports whether checks for the host should be skipped right now.
func (s *Store) IsPaused(hostName string) bool {
	_, ok := s.Get(hostName)
	return ok
}

// Get returns the active pause for a host, or false if none or expired.
func (s *Store) Get(hostName string) (Pause, bool) {
	if s == nil {
		return Pause{}, false
	}

	s.mu.RLock()
	p, ok := s.m[hostName]
	s.mu.RUnlock()
	if !ok {
		return Pause{}, false
	}
	if p.Until != nil && p.Until.Before(time.Now()) {
		_ = s.Clear(hostName)
		return Pause{}, false
	}
	return p, true
}

// Set activates or replaces a pause for the host.
func (s *Store) Set(hostName, preset string, until *time.Time) error {
	if s == nil {
		return fmt.Errorf("pause store not initialized")
	}
	if hostName == "" {
		return fmt.Errorf("host name required")
	}
	if preset == "" {
		return fmt.Errorf("preset required")
	}

	var untilUnix sql.NullInt64
	if until != nil {
		untilUnix = sql.NullInt64{Int64: until.Unix(), Valid: true}
	}

	if _, err := s.db.Exec(
		`INSERT INTO host_check_pauses (host_name, until_unix, preset) VALUES (?, ?, ?)
		 ON CONFLICT(host_name) DO UPDATE SET until_unix = excluded.until_unix, preset = excluded.preset`,
		hostName, untilUnix, preset,
	); err != nil {
		return fmt.Errorf("set pause: %w", err)
	}

	p := Pause{Preset: preset}
	if until != nil {
		t := *until
		p.Until = &t
	}

	s.mu.Lock()
	s.m[hostName] = p
	s.mu.Unlock()
	return nil
}

// Clear removes the pause for a host.
func (s *Store) Clear(hostName string) error {
	if s == nil {
		return nil
	}
	if hostName == "" {
		return nil
	}

	if _, err := s.db.Exec(`DELETE FROM host_check_pauses WHERE host_name = ?`, hostName); err != nil {
		return fmt.Errorf("clear pause: %w", err)
	}

	s.mu.Lock()
	delete(s.m, hostName)
	s.mu.Unlock()
	return nil
}

// Rename moves a pause entry when a host is renamed.
func (s *Store) Rename(oldName, newName string) error {
	if s == nil || oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	s.mu.RLock()
	p, ok := s.m[oldName]
	s.mu.RUnlock()
	if !ok {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM host_check_pauses WHERE host_name = ?`, oldName); err != nil {
		return fmt.Errorf("delete old pause: %w", err)
	}

	var untilUnix sql.NullInt64
	if p.Until != nil {
		untilUnix = sql.NullInt64{Int64: p.Until.Unix(), Valid: true}
	}
	if _, err := tx.Exec(
		`INSERT INTO host_check_pauses (host_name, until_unix, preset) VALUES (?, ?, ?)`,
		newName, untilUnix, p.Preset,
	); err != nil {
		return fmt.Errorf("insert renamed pause: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rename: %w", err)
	}

	s.mu.Lock()
	delete(s.m, oldName)
	s.m[newName] = p
	s.mu.Unlock()
	return nil
}

// Prune removes pause entries for hosts that no longer exist.
func (s *Store) Prune(known []string) error {
	if s == nil {
		return nil
	}

	keep := make(map[string]struct{}, len(known))
	for _, name := range known {
		keep[name] = struct{}{}
	}

	s.mu.RLock()
	var stale []string
	for name := range s.m {
		if _, ok := keep[name]; !ok {
			stale = append(stale, name)
		}
	}
	s.mu.RUnlock()

	for _, name := range stale {
		if err := s.Clear(name); err != nil {
			return err
		}
	}
	return nil
}

// PresetDuration returns the duration for a preset, or false for forever.
func PresetDuration(preset string) (time.Duration, bool) {
	d, ok := presetDurations[preset]
	return d, ok
}

// SetFromPreset sets a pause using a named preset from now.
func (s *Store) SetFromPreset(hostName, preset string) error {
	if preset == PresetForever {
		return s.Set(hostName, preset, nil)
	}
	d, ok := PresetDuration(preset)
	if !ok {
		return fmt.Errorf("unknown preset %q", preset)
	}
	until := time.Now().Add(d)
	return s.Set(hostName, preset, &until)
}
