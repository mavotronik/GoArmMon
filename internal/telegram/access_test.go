package telegram

import (
	"path/filepath"
	"testing"

	"goarmmon/internal/acl"
	"goarmmon/internal/config"
	"goarmmon/internal/state"
)

func testBotACL(t *testing.T, root int64) (*Bot, *acl.Store) {
	t.Helper()
	st, err := acl.Open(filepath.Join(t.TempDir(), "acl.sqlite"), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	b := New(config.TelegramConfig{AllowedUsers: []int64{root}}, state.NewCache())
	b.SetACL(st)
	return b, st
}

func TestBotPermissions(t *testing.T) {
	const rootID int64 = 1
	b, st := testBotACL(t, rootID)
	if err := st.AddUser(2, acl.RoleUser); err != nil {
		t.Fatal(err)
	}
	if err := st.AddUser(3, acl.RoleLimitedAdmin); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOwner("mine", 3); err != nil {
		t.Fatal(err)
	}

	if !b.canManageUsers(rootID) || !b.canAddHost(rootID) || !b.canEditHost(rootID, "unowned") {
		t.Fatal("root should have full rights")
	}
	if b.canAddHost(2) || b.canEditHost(2, "mine") || b.canViewHost(2, "mine") || b.canManageUsers(2) {
		t.Fatal("user should be view/edit denied by default")
	}
	if !b.canAddHost(3) || !b.canEditHost(3, "mine") || b.canEditHost(3, "unowned") || b.canManageUsers(3) {
		t.Fatal("limited_admin should edit only owned hosts")
	}
	if b.accessSummary(2) != "user access=selected hosts=none" {
		t.Fatalf("user access summary = %q", b.accessSummary(2))
	}
	if b.accessSummary(rootID) != "root all" {
		t.Fatalf("root access summary = %q", b.accessSummary(rootID))
	}
}

func TestParseUserCallbacks(t *testing.T) {
	id, role, ok := parseUserIDRole("42:limited_admin")
	if !ok || id != 42 || role != acl.RoleLimitedAdmin {
		t.Fatalf("parseUserIDRole = %d %q %v", id, role, ok)
	}
	id, acc, ok := parseUserIDAccess("42:selected")
	if !ok || id != 42 || acc != acl.HostAccessSelected {
		t.Fatalf("parseUserIDAccess = %d %q %v", id, acc, ok)
	}
	id, idx, ok := parseUserIDIdx("42:3")
	if !ok || id != 42 || idx != 3 {
		t.Fatalf("parseUserIDIdx = %d %d %v", id, idx, ok)
	}
}
