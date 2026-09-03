package acl

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type Role string

const (
	RoleRoot         Role = "root"
	RoleUser         Role = "user"
	RoleLimitedAdmin Role = "limited_admin"
)

type HostAccess string

const (
	HostAccessAll      HostAccess = "all"
	HostAccessSelected HostAccess = "selected"
)

// User is a persisted ACL record. The config primary root is not stored here.
type User struct {
	ID         int64
	Role       Role
	HostAccess HostAccess
	Hosts      []string
	OwnedHosts []string
}

type Store struct {
	db          *sql.DB
	mu          sync.RWMutex
	primaryRoot int64
}

func Open(dbFile string, primaryRoot int64, extraIDs []int64) (*Store, error) {
	if primaryRoot == 0 {
		return nil, fmt.Errorf("primary root id is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.ToSlash(dbFile))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db, primaryRoot: primaryRoot}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedExtras(extraIDs); err != nil {
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

func (s *Store) ReloadPrimary(primaryRoot int64, extraIDs []int64) error {
	if primaryRoot == 0 {
		return fmt.Errorf("primary root id is required")
	}
	s.mu.Lock()
	s.primaryRoot = primaryRoot
	s.mu.Unlock()
	return s.seedExtras(extraIDs)
}

func (s *Store) PrimaryRoot() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.primaryRoot
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	role TEXT NOT NULL CHECK(role IN ('root', 'user', 'limited_admin')),
	host_access TEXT NOT NULL CHECK(host_access IN ('all', 'selected'))
);
CREATE TABLE IF NOT EXISTS user_hosts (
	user_id INTEGER NOT NULL,
	host_name TEXT NOT NULL,
	PRIMARY KEY (user_id, host_name),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS host_owners (
	host_name TEXT PRIMARY KEY,
	owner_id INTEGER NOT NULL
);
`)
	return err
}

func (s *Store) seedExtras(ids []int64) error {
	root := s.PrimaryRoot()
	for _, id := range ids {
		if id == 0 || id == root {
			continue
		}
		_, err := s.db.Exec(`INSERT OR IGNORE INTO users (id, role, host_access) VALUES (?, ?, ?)`, id, string(RoleUser), string(HostAccessSelected))
		if err != nil {
			return fmt.Errorf("seed user %d: %w", id, err)
		}
	}
	return nil
}

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleRoot, RoleUser, RoleLimitedAdmin:
		return Role(s), nil
	default:
		return "", fmt.Errorf("unknown role %q", s)
	}
}

func (s *Store) Allowed(id int64) bool {
	if id == s.PrimaryRoot() {
		return true
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id = ?`, id).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) Role(id int64) Role {
	if id == s.PrimaryRoot() {
		return RoleRoot
	}
	var role string
	err := s.db.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role)
	if err != nil {
		return ""
	}
	return Role(role)
}

func (s *Store) CanManageUsers(id int64) bool {
	return s.Role(id) == RoleRoot
}

func (s *Store) CanAddHost(id int64) bool {
	switch s.Role(id) {
	case RoleRoot, RoleLimitedAdmin:
		return true
	default:
		return false
	}
}

func (s *Store) CanViewHost(id int64, name string) bool {
	switch s.Role(id) {
	case RoleRoot:
		return true
	case RoleUser:
		return s.inViewScope(id, name)
	case RoleLimitedAdmin:
		return s.inViewScope(id, name) || s.owns(id, name)
	default:
		return false
	}
}

func (s *Store) CanEditHost(id int64, name string) bool {
	switch s.Role(id) {
	case RoleRoot:
		return true
	case RoleLimitedAdmin:
		return s.owns(id, name)
	default:
		return false
	}
}

func (s *Store) inViewScope(id int64, name string) bool {
	var access string
	err := s.db.QueryRow(`SELECT host_access FROM users WHERE id = ?`, id).Scan(&access)
	if err != nil {
		return false
	}
	if HostAccess(access) == HostAccessAll {
		return true
	}
	var n int
	err = s.db.QueryRow(`SELECT COUNT(1) FROM user_hosts WHERE user_id = ? AND host_name = ?`, id, name).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) owns(id int64, name string) bool {
	var owner int64
	err := s.db.QueryRow(`SELECT owner_id FROM host_owners WHERE host_name = ?`, name).Scan(&owner)
	return err == nil && owner == id
}

func (s *Store) Owner(name string) (int64, bool) {
	var owner int64
	err := s.db.QueryRow(`SELECT owner_id FROM host_owners WHERE host_name = ?`, name).Scan(&owner)
	if err != nil {
		return 0, false
	}
	return owner, true
}

func (s *Store) SetOwner(hostName string, ownerID int64) error {
	_, err := s.db.Exec(`INSERT INTO host_owners (host_name, owner_id) VALUES (?, ?)
		ON CONFLICT(host_name) DO UPDATE SET owner_id = excluded.owner_id`, hostName, ownerID)
	return err
}

func (s *Store) RenameHost(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE host_owners SET host_name = ? WHERE host_name = ?`, newName, oldName); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_hosts SET host_name = ? WHERE host_name = ?`, newName, oldName); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteHost(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM host_owners WHERE host_name = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_hosts WHERE host_name = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AllowedIDs() []int64 {
	root := s.PrimaryRoot()
	ids := []int64{root}
	rows, err := s.db.Query(`SELECT id FROM users ORDER BY id`)
	if err != nil {
		return ids
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return ids
		}
		if id == root {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) AccessSummary(id int64) string {
	role := s.Role(id)
	if role == "" {
		return "none"
	}
	if role == RoleRoot {
		return "root all"
	}
	u, ok := s.GetUser(id)
	if !ok {
		return string(role)
	}
	parts := []string{string(u.Role), "access=" + string(u.HostAccess)}
	if u.HostAccess == HostAccessSelected {
		if len(u.Hosts) == 0 {
			parts = append(parts, "hosts=none")
		} else {
			parts = append(parts, "hosts="+strings.Join(u.Hosts, ","))
		}
	}
	if len(u.OwnedHosts) > 0 {
		parts = append(parts, "owned="+strings.Join(u.OwnedHosts, ","))
	}
	return strings.Join(parts, " ")
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, role, host_access FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	root := s.PrimaryRoot()
	var out []User
	for rows.Next() {
		var u User
		var role, access string
		if err := rows.Scan(&u.ID, &role, &access); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if u.ID == root {
			continue
		}
		u.Role = Role(role)
		u.HostAccess = HostAccess(access)
		out = append(out, u)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Hosts = s.selectedHosts(out[i].ID)
		out[i].OwnedHosts = s.ownedHosts(out[i].ID)
	}
	return out, nil
}

func (s *Store) GetUser(id int64) (User, bool) {
	if id == s.PrimaryRoot() {
		return User{ID: id, Role: RoleRoot, HostAccess: HostAccessAll}, true
	}
	var u User
	var role, access string
	err := s.db.QueryRow(`SELECT id, role, host_access FROM users WHERE id = ?`, id).Scan(&u.ID, &role, &access)
	if err != nil {
		return User{}, false
	}
	u.Role = Role(role)
	u.HostAccess = HostAccess(access)
	u.Hosts = s.selectedHosts(id)
	u.OwnedHosts = s.ownedHosts(id)
	return u, true
}

func (s *Store) selectedHosts(id int64) []string {
	rows, err := s.db.Query(`SELECT host_name FROM user_hosts WHERE user_id = ? ORDER BY host_name`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out
		}
		out = append(out, name)
	}
	return out
}

func (s *Store) ownedHosts(id int64) []string {
	rows, err := s.db.Query(`SELECT host_name FROM host_owners WHERE owner_id = ? ORDER BY host_name`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out
		}
		out = append(out, name)
	}
	return out
}

func (s *Store) AddUser(id int64, role Role) error {
	if id == 0 {
		return fmt.Errorf("user id is required")
	}
	if id == s.PrimaryRoot() {
		return fmt.Errorf("cannot add config root as a database user")
	}
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO users (id, role, host_access) VALUES (?, ?, ?)`, id, string(role), string(HostAccessSelected))
	if err != nil {
		return fmt.Errorf("add user: %w", err)
	}
	return nil
}

func (s *Store) SetRole(id int64, role Role) error {
	if id == s.PrimaryRoot() {
		return fmt.Errorf("cannot change config root role")
	}
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, string(role), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

func (s *Store) SetHostAccess(id int64, access HostAccess) error {
	if id == s.PrimaryRoot() {
		return fmt.Errorf("cannot change config root host access")
	}
	if access != HostAccessAll && access != HostAccessSelected {
		return fmt.Errorf("unknown host access %q", access)
	}
	res, err := s.db.Exec(`UPDATE users SET host_access = ? WHERE id = ?`, string(access), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

func (s *Store) ToggleSelectedHost(id int64, name string) error {
	if id == s.PrimaryRoot() {
		return fmt.Errorf("cannot change config root host list")
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM user_hosts WHERE user_id = ? AND host_name = ?`, id, name).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		_, err = s.db.Exec(`DELETE FROM user_hosts WHERE user_id = ? AND host_name = ?`, id, name)
		return err
	}
	_, err = s.db.Exec(`INSERT INTO user_hosts (user_id, host_name) VALUES (?, ?)`, id, name)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	if id == s.PrimaryRoot() {
		return fmt.Errorf("cannot delete config root")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM host_owners WHERE owner_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return tx.Commit()
}
