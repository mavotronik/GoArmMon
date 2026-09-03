package acl

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T, root int64, extras []int64) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acl.sqlite")
	s, err := Open(path, root, extras)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPrimaryRootAlwaysRoot(t *testing.T) {
	s := openTestStore(t, 10, []int64{20})
	if !s.Allowed(10) || s.Role(10) != RoleRoot {
		t.Fatalf("primary: allowed=%v role=%q", s.Allowed(10), s.Role(10))
	}
	if !s.CanManageUsers(10) || !s.CanAddHost(10) || !s.CanEditHost(10, "any") || !s.CanViewHost(10, "any") {
		t.Fatal("root should have full access")
	}
	if err := s.AddUser(10, RoleUser); err == nil {
		t.Fatal("should not add primary root to db")
	}
	if err := s.SetRole(10, RoleUser); err == nil {
		t.Fatal("should not change primary root role")
	}
	if err := s.DeleteUser(10); err == nil {
		t.Fatal("should not delete primary root")
	}
}

func TestSeedExtrasDefaultUserNone(t *testing.T) {
	s := openTestStore(t, 10, []int64{20, 10, 0})
	if s.Role(20) != RoleUser {
		t.Fatalf("seeded role = %q", s.Role(20))
	}
	if s.CanViewHost(20, "router") {
		t.Fatal("new user should see no hosts")
	}
	if s.CanAddHost(20) || s.CanEditHost(20, "router") {
		t.Fatal("user should not add/edit hosts")
	}
	users, err := s.ListUsers()
	if err != nil || len(users) != 1 || users[0].ID != 20 {
		t.Fatalf("list = %+v err=%v", users, err)
	}
}

func TestSeedIgnoreExisting(t *testing.T) {
	s := openTestStore(t, 10, []int64{20})
	if err := s.SetRole(20, RoleLimitedAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadPrimary(10, []int64{20}); err != nil {
		t.Fatal(err)
	}
	if s.Role(20) != RoleLimitedAdmin {
		t.Fatal("reload seed should not overwrite existing user")
	}
}

func TestHostAccessAllVsSelected(t *testing.T) {
	s := openTestStore(t, 1, nil)
	if err := s.AddUser(2, RoleUser); err != nil {
		t.Fatal(err)
	}
	if s.CanViewHost(2, "a") {
		t.Fatal("selected empty should see nothing")
	}
	if err := s.ToggleSelectedHost(2, "a"); err != nil {
		t.Fatal(err)
	}
	if !s.CanViewHost(2, "a") || s.CanViewHost(2, "b") {
		t.Fatal("selected should only include toggled hosts")
	}
	if err := s.SetHostAccess(2, HostAccessAll); err != nil {
		t.Fatal(err)
	}
	if !s.CanViewHost(2, "b") {
		t.Fatal("access=all should see every host")
	}
}

func TestLimitedAdminOwnsOnlyOwnHosts(t *testing.T) {
	s := openTestStore(t, 1, nil)
	if err := s.AddUser(3, RoleLimitedAdmin); err != nil {
		t.Fatal(err)
	}
	if !s.CanAddHost(3) {
		t.Fatal("limited_admin should add hosts")
	}
	if s.CanEditHost(3, "unowned") || s.CanViewHost(3, "unowned") {
		t.Fatal("should not see/edit unowned hosts without scope")
	}
	if err := s.SetOwner("mine", 3); err != nil {
		t.Fatal(err)
	}
	if !s.CanEditHost(3, "mine") || !s.CanViewHost(3, "mine") {
		t.Fatal("should view and edit owned host")
	}
	if err := s.ToggleSelectedHost(3, "other"); err != nil {
		t.Fatal(err)
	}
	if !s.CanViewHost(3, "other") || s.CanEditHost(3, "other") {
		t.Fatal("scoped host is view-only")
	}
}

func TestRenameAndDeleteHost(t *testing.T) {
	s := openTestStore(t, 1, nil)
	if err := s.AddUser(4, RoleUser); err != nil {
		t.Fatal(err)
	}
	if err := s.ToggleSelectedHost(4, "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwner("old", 4); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameHost("old", "new"); err != nil {
		t.Fatal(err)
	}
	if s.CanViewHost(4, "old") {
		t.Fatal("old name should be gone")
	}
	if !s.CanViewHost(4, "new") {
		t.Fatal("new name should remain in scope")
	}
	owner, ok := s.Owner("new")
	if !ok || owner != 4 {
		t.Fatalf("owner after rename = %d ok=%v", owner, ok)
	}
	if err := s.DeleteHost("new"); err != nil {
		t.Fatal(err)
	}
	if s.CanViewHost(4, "new") {
		t.Fatal("deleted host should leave scope")
	}
	if _, ok := s.Owner("new"); ok {
		t.Fatal("owner row should be removed")
	}
}

func TestDeleteUserRemovesOwnership(t *testing.T) {
	s := openTestStore(t, 1, nil)
	if err := s.AddUser(5, RoleLimitedAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwner("h", 5); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(5); err != nil {
		t.Fatal(err)
	}
	if s.Allowed(5) {
		t.Fatal("deleted user should not be allowed")
	}
	if _, ok := s.Owner("h"); ok {
		t.Fatal("ownership should be cleared")
	}
}

func TestAccessSummary(t *testing.T) {
	s := openTestStore(t, 1, nil)
	if got := s.AccessSummary(1); got != "root all" {
		t.Fatalf("root summary = %q", got)
	}
	if got := s.AccessSummary(99); got != "none" {
		t.Fatalf("unknown summary = %q", got)
	}
	if err := s.AddUser(2, RoleUser); err != nil {
		t.Fatal(err)
	}
	if got := s.AccessSummary(2); got != "user access=selected hosts=none" {
		t.Fatalf("user summary = %q", got)
	}
}

func TestAllowedIDs(t *testing.T) {
	s := openTestStore(t, 10, []int64{20})
	ids := s.AllowedIDs()
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 20 {
		t.Fatalf("AllowedIDs = %v", ids)
	}
}
