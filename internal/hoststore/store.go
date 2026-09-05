package hoststore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"goarmmon/internal/config"

	"gopkg.in/yaml.v3"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type ImportResult struct {
	Imported []string
	Skipped  []string
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

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
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
CREATE TABLE IF NOT EXISTS hosts (
	name TEXT PRIMARY KEY,
	yaml TEXT NOT NULL,
	sort_order INTEGER NOT NULL
);
`)
	return err
}

func (s *Store) List() ([]config.HostConfig, error) {
	rows, err := s.db.Query(`SELECT yaml FROM hosts ORDER BY sort_order, name`)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	defer rows.Close()

	var hosts []config.HostConfig
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, fmt.Errorf("scan host: %w", err)
		}
		var h config.HostConfig
		if err := yaml.Unmarshal([]byte(blob), &h); err != nil {
			return nil, fmt.Errorf("unmarshal host %q: %w", h.Name, err)
		}
		if err := config.PrepareHost(&h); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	return hosts, nil
}

func (s *Store) ReplaceAll(hosts []config.HostConfig) error {
	if err := config.ValidateHostList(hosts); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM hosts`); err != nil {
		return fmt.Errorf("clear hosts: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO hosts (name, yaml, sort_order) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for i, h := range hosts {
		blob, err := yaml.Marshal(h)
		if err != nil {
			return fmt.Errorf("marshal host %q: %w", h.Name, err)
		}
		if _, err := stmt.Exec(h.Name, string(blob), i); err != nil {
			return fmt.Errorf("insert host %q: %w", h.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit hosts: %w", err)
	}
	return nil
}

func (s *Store) ImportNew(hosts []config.HostConfig) (ImportResult, error) {
	var result ImportResult
	if len(hosts) == 0 {
		return result, nil
	}
	if err := config.ValidateHostList(hosts); err != nil {
		return result, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var maxOrder int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM hosts`).Scan(&maxOrder); err != nil {
		return result, fmt.Errorf("max sort order: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO hosts (name, yaml, sort_order) VALUES (?, ?, ?)`)
	if err != nil {
		return result, fmt.Errorf("prepare import: %w", err)
	}
	defer stmt.Close()

	nextOrder := maxOrder + 1
	for _, h := range hosts {
		blob, err := yaml.Marshal(h)
		if err != nil {
			return result, fmt.Errorf("marshal host %q: %w", h.Name, err)
		}
		res, err := stmt.Exec(h.Name, string(blob), nextOrder)
		if err != nil {
			return result, fmt.Errorf("import host %q: %w", h.Name, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("rows affected for host %q: %w", h.Name, err)
		}
		if n > 0 {
			result.Imported = append(result.Imported, h.Name)
			nextOrder++
			continue
		}
		result.Skipped = append(result.Skipped, h.Name)
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}
