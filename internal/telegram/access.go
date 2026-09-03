package telegram

import (
	"log/slog"
	"sort"

	"goarmmon/internal/acl"
	"goarmmon/internal/state"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) SetACL(store *acl.Store) {
	b.acl = store
}

func (b *Bot) allowedUser(id int64) bool {
	if b.acl != nil {
		return b.acl.Allowed(id)
	}
	_, ok := b.allowed[id]
	return ok
}

func (b *Bot) roleOf(id int64) acl.Role {
	if b.acl != nil {
		return b.acl.Role(id)
	}
	if b.allowedUser(id) {
		return acl.RoleRoot
	}
	return ""
}

func (b *Bot) accessSummary(id int64) string {
	if b.acl != nil {
		return b.acl.AccessSummary(id)
	}
	if b.allowedUser(id) {
		return "root all"
	}
	return "none"
}

func (b *Bot) canViewHost(id int64, name string) bool {
	if b.acl != nil {
		return b.acl.CanViewHost(id, name)
	}
	return b.allowedUser(id)
}

func (b *Bot) canEditHost(id int64, name string) bool {
	if b.acl != nil {
		return b.acl.CanEditHost(id, name)
	}
	return b.allowedUser(id)
}

func (b *Bot) canAddHost(id int64) bool {
	if b.acl != nil {
		return b.acl.CanAddHost(id)
	}
	return b.allowedUser(id)
}

func (b *Bot) canManageUsers(id int64) bool {
	if b.acl != nil {
		return b.acl.CanManageUsers(id)
	}
	return b.allowedUser(id)
}

func (b *Bot) allowedIDs() []int64 {
	if b.acl != nil {
		return b.acl.AllowedIDs()
	}
	ids := make([]int64, 0, len(b.allowed))
	for id := range b.allowed {
		ids = append(ids, id)
	}
	return ids
}

func (b *Bot) visibleHosts(userID int64) []state.HostView {
	hosts := b.cache.ListHosts()
	out := make([]state.HostView, 0, len(hosts))
	for _, h := range hosts {
		if b.canViewHost(userID, h.Name) {
			out = append(out, h)
		}
	}
	return out
}

func (b *Bot) filterSnaps(userID int64, snaps []state.CheckSnapshot) []state.CheckSnapshot {
	out := make([]state.CheckSnapshot, 0, len(snaps))
	for _, s := range snaps {
		if b.canViewHost(userID, s.HostName) {
			out = append(out, s)
		}
	}
	return out
}

func (b *Bot) visibleGroups(userID int64) []string {
	seen := make(map[string]struct{})
	var groups []string
	for _, h := range b.visibleHosts(userID) {
		if h.Group == "" {
			continue
		}
		if _, ok := seen[h.Group]; ok {
			continue
		}
		seen[h.Group] = struct{}{}
		groups = append(groups, h.Group)
	}
	sort.Strings(groups)
	return groups
}

func (b *Bot) logRequest(from *tgbotapi.User, kind, detail string) {
	if from == nil {
		return
	}
	role := b.roleOf(from.ID)
	if role == "" {
		role = "denied"
	}
	attrs := []any{
		"user_id", from.ID,
		"role", string(role),
		"access", b.accessSummary(from.ID),
		"kind", kind,
		"detail", detail,
	}
	if from.UserName != "" {
		attrs = append(attrs, "username", from.UserName)
	}
	slog.Info("telegram request", attrs...)
}

func (b *Bot) logDenied(userID int64, action, host string) {
	role := b.roleOf(userID)
	if role == "" {
		role = "denied"
	}
	slog.Warn("telegram access denied",
		"user_id", userID,
		"role", string(role),
		"access", b.accessSummary(userID),
		"action", action,
		"host", host,
	)
}

func (b *Bot) denyUnlessCanAdd(userID int64) (text string, markup *tgbotapi.InlineKeyboardMarkup, denied bool) {
	if b.canAddHost(userID) {
		return "", nil, false
	}
	b.logDenied(userID, "host:add", "")
	return "Access denied.", b.hostsMenuKeyboard(userID), true
}

func (b *Bot) denyUnlessCanEditIdx(userID int64, idx int) (text string, markup *tgbotapi.InlineKeyboardMarkup, denied bool) {
	if b.store == nil {
		return "Host store unavailable.", mainMenuKeyboard(), true
	}
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "Host not found.", b.hostsMenuKeyboard(userID), true
	}
	if b.canEditHost(userID, hosts[idx].Name) {
		return "", nil, false
	}
	b.logDenied(userID, "edit_host", hosts[idx].Name)
	return "Access denied.", b.hostsMenuKeyboard(userID), true
}

func (b *Bot) pendingInputMode(userID int64) string {
	if b.userEdit != nil {
		if b.userEdit.hasAdd(userID) {
			return "add_user"
		}
	}
	if b.hostEdit != nil {
		if s, ok := b.hostEdit.get(userID); ok {
			return s.mode
		}
	}
	return ""
}
