package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"goarmmon/internal/acl"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cbMenuUsers   = "menu:users"
	cbUsersAdd    = "users:add"
	cbUsersCard   = "users:u:"
	cbUsersRole   = "users:role:"
	cbUsersAccess = "users:acc:"
	cbUsersHost   = "users:h:"
	cbUsersDel    = "users:del:"
	cbUsersDelYes = "users:dely:"
	cbUsersDelNo  = "users:deln:"
)

type userEditor struct {
	mu      sync.Mutex
	pending map[int64]time.Time
}

func newUserEditor() *userEditor {
	return &userEditor{pending: make(map[int64]time.Time)}
}

func (e *userEditor) setAdd(userID int64) {
	e.mu.Lock()
	e.pending[userID] = time.Now().Add(sessionTTL)
	e.mu.Unlock()
}

func (e *userEditor) hasAdd(userID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	exp, ok := e.pending[userID]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(e.pending, userID)
		return false
	}
	return true
}

func (e *userEditor) takeAdd(userID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	exp, ok := e.pending[userID]
	if !ok {
		return false
	}
	delete(e.pending, userID)
	return time.Now().Before(exp)
}

func (e *userEditor) clear(userID int64) {
	e.mu.Lock()
	delete(e.pending, userID)
	e.mu.Unlock()
}

func (b *Bot) handleUsersCallback(data string, userID int64) (text string, markup *tgbotapi.InlineKeyboardMarkup, handled bool) {
	if !strings.HasPrefix(data, "users:") && data != cbMenuUsers {
		return "", nil, false
	}
	if !b.canManageUsers(userID) {
		b.logDenied(userID, data, "")
		return "Access denied.", b.settingsMenuKeyboard(userID), true
	}
	if b.acl == nil {
		return "ACL store is not available.", b.settingsMenuKeyboard(userID), true
	}

	switch {
	case data == cbMenuUsers:
		return b.formatUsersList(), b.usersListKeyboard(), true
	case data == cbUsersAdd:
		b.userEdit.setAdd(userID)
		return "<b>Add user</b>\nSend the Telegram user ID (numeric):", nil, true
	case strings.HasPrefix(data, cbUsersCard):
		id, ok := parseUserID(strings.TrimPrefix(data, cbUsersCard))
		if !ok {
			return "Invalid user.", b.usersListKeyboard(), true
		}
		return b.formatUserCard(id), b.userCardKeyboard(id), true
	case strings.HasPrefix(data, cbUsersRole):
		id, role, ok := parseUserIDRole(strings.TrimPrefix(data, cbUsersRole))
		if !ok {
			return "Invalid action.", b.usersListKeyboard(), true
		}
		if err := b.acl.SetRole(id, role); err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.userCardKeyboard(id), true
		}
		return b.formatUserCard(id), b.userCardKeyboard(id), true
	case strings.HasPrefix(data, cbUsersAccess):
		id, access, ok := parseUserIDAccess(strings.TrimPrefix(data, cbUsersAccess))
		if !ok {
			return "Invalid action.", b.usersListKeyboard(), true
		}
		if err := b.acl.SetHostAccess(id, access); err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.userCardKeyboard(id), true
		}
		return b.formatUserCard(id), b.userCardKeyboard(id), true
	case strings.HasPrefix(data, cbUsersHost):
		id, idx, ok := parseUserIDIdx(strings.TrimPrefix(data, cbUsersHost))
		if !ok {
			return "Invalid action.", b.usersListKeyboard(), true
		}
		hosts := b.store.HostConfigs()
		if idx < 0 || idx >= len(hosts) {
			return "Host not found.", b.userCardKeyboard(id), true
		}
		if err := b.acl.ToggleSelectedHost(id, hosts[idx].Name); err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.userCardKeyboard(id), true
		}
		return b.formatUserCard(id), b.userCardKeyboard(id), true
	case strings.HasPrefix(data, cbUsersDelYes):
		id, ok := parseUserID(strings.TrimPrefix(data, cbUsersDelYes))
		if !ok {
			return "Invalid user.", b.usersListKeyboard(), true
		}
		if err := b.acl.DeleteUser(id); err != nil {
			return fmt.Sprintf("Delete failed: %s", escapeHTML(err.Error())), b.usersListKeyboard(), true
		}
		return "<b>User deleted.</b>\n\n" + b.formatUsersList(), b.usersListKeyboard(), true
	case strings.HasPrefix(data, cbUsersDelNo):
		id, ok := parseUserID(strings.TrimPrefix(data, cbUsersDelNo))
		if !ok {
			return "Invalid user.", b.usersListKeyboard(), true
		}
		return b.formatUserCard(id), b.userCardKeyboard(id), true
	case strings.HasPrefix(data, cbUsersDel):
		id, ok := parseUserID(strings.TrimPrefix(data, cbUsersDel))
		if !ok {
			return "Invalid user.", b.usersListKeyboard(), true
		}
		return fmt.Sprintf("<b>Delete user %d?</b>\nThey will lose bot access. This cannot be undone.", id),
			b.userDeleteConfirmKeyboard(id), true
	}
	return "", nil, false
}

func (b *Bot) handleUserTextInput(msg *tgbotapi.Message) bool {
	if b.userEdit == nil || !b.userEdit.takeAdd(msg.From.ID) {
		return false
	}
	if !b.canManageUsers(msg.From.ID) {
		b.logDenied(msg.From.ID, "users:add", "")
		b.replyPlain(msg.Chat.ID, "Access denied.")
		return true
	}
	if b.acl == nil {
		b.replyPlain(msg.Chat.ID, "ACL store is not available.")
		return true
	}
	id, err := strconv.ParseInt(strings.TrimSpace(msg.Text), 10, 64)
	if err != nil || id <= 0 {
		b.userEdit.setAdd(msg.From.ID)
		b.replyPlain(msg.Chat.ID, "Invalid Telegram user ID. Send a positive number or /cancel.")
		return true
	}
	if err := b.acl.AddUser(id, acl.RoleUser); err != nil {
		b.replyPlain(msg.Chat.ID, fmt.Sprintf("Failed to add user: %s", err.Error()))
		return true
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, b.formatUserCard(id))
	reply.ParseMode = tgbotapi.ModeHTML
	reply.ReplyMarkup = b.userCardKeyboard(id)
	if _, err := b.send(reply); err != nil {
		slogWarnSend(err)
	}
	return true
}

func (b *Bot) formatUsersList() string {
	var sb strings.Builder
	sb.WriteString("<b>Users</b>\n")
	root := int64(0)
	if b.acl != nil {
		root = b.acl.PrimaryRoot()
	} else if len(b.cfg.AllowedUsers) > 0 {
		root = b.cfg.AllowedUsers[0]
	}
	sb.WriteString(fmt.Sprintf("• <code>%d</code> — root (config)\n", root))
	if b.acl == nil {
		return sb.String()
	}
	users, err := b.acl.ListUsers()
	if err != nil {
		sb.WriteString("\nFailed to list users: " + escapeHTML(err.Error()))
		return sb.String()
	}
	if len(users) == 0 {
		sb.WriteString("\nNo additional users. Use Add user.")
		return sb.String()
	}
	for _, u := range users {
		sb.WriteString(fmt.Sprintf("• <code>%d</code> — %s, %s\n", u.ID, u.Role, hostAccessLabel(u)))
	}
	return sb.String()
}

func hostAccessLabel(u acl.User) string {
	if u.Role == acl.RoleRoot || u.HostAccess == acl.HostAccessAll {
		return "hosts: all"
	}
	if len(u.Hosts) == 0 {
		return "hosts: none"
	}
	return fmt.Sprintf("hosts: %s", strings.Join(u.Hosts, ", "))
}

func (b *Bot) formatUserCard(id int64) string {
	if b.acl != nil && id == b.acl.PrimaryRoot() {
		return fmt.Sprintf("<b>User %d</b>\nRole: <b>root</b> (from config)\nHosts: all\n\nThis account cannot be changed here.", id)
	}
	u, ok := b.acl.GetUser(id)
	if !ok {
		return fmt.Sprintf("User %d not found.", id)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>User %d</b>\n", u.ID))
	sb.WriteString(fmt.Sprintf("Role: <b>%s</b>\n", u.Role))
	sb.WriteString("Host access: " + string(u.HostAccess) + "\n")
	if u.HostAccess == acl.HostAccessSelected {
		if len(u.Hosts) == 0 {
			sb.WriteString("Selected hosts: none\n")
		} else {
			sb.WriteString("Selected hosts: " + strings.Join(u.Hosts, ", ") + "\n")
		}
	}
	if len(u.OwnedHosts) > 0 {
		sb.WriteString("Owned hosts: " + strings.Join(u.OwnedHosts, ", ") + "\n")
	}
	sb.WriteString("\nNew users start with no hosts until you grant All or pick a list.")
	return sb.String()
}

func (b *Bot) usersListKeyboard() *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("« Settings", cbMenuSettings)},
		{tgbotapi.NewInlineKeyboardButtonData("Add user", cbUsersAdd)},
	}
	root := int64(0)
	if b.acl != nil {
		root = b.acl.PrimaryRoot()
	}
	if root != 0 {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("root %d", root), cbUsersCard+strconv.FormatInt(root, 10)),
		})
	}
	if b.acl != nil {
		if users, err := b.acl.ListUsers(); err == nil {
			for _, u := range users {
				label := fmt.Sprintf("%s %d", u.Role, u.ID)
				rows = append(rows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(label, cbUsersCard+strconv.FormatInt(u.ID, 10)),
				})
			}
		}
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) userCardKeyboard(id int64) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("« Users", cbMenuUsers)},
	}
	if b.acl != nil && id == b.acl.PrimaryRoot() {
		markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
		return &markup
	}
	u, ok := b.acl.GetUser(id)
	if !ok {
		markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
		return &markup
	}
	idStr := strconv.FormatInt(id, 10)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		roleButton(idStr, "root", u.Role == acl.RoleRoot),
		roleButton(idStr, "user", u.Role == acl.RoleUser),
	})
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		roleButton(idStr, "limited_admin", u.Role == acl.RoleLimitedAdmin),
	})
	allLabel := "Hosts: all"
	selLabel := "Hosts: selected"
	if u.HostAccess == acl.HostAccessAll {
		allLabel = "• " + allLabel
	} else {
		selLabel = "• " + selLabel
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(allLabel, cbUsersAccess+idStr+":all"),
		tgbotapi.NewInlineKeyboardButtonData(selLabel, cbUsersAccess+idStr+":selected"),
	})
	if u.HostAccess == acl.HostAccessSelected && b.store != nil {
		hosts := b.store.HostConfigs()
		selected := make(map[string]struct{}, len(u.Hosts))
		for _, name := range u.Hosts {
			selected[name] = struct{}{}
		}
		for i, h := range hosts {
			label := h.Name
			if _, ok := selected[h.Name]; ok {
				label = "• " + label
			}
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(label, cbUsersHost+idStr+":"+strconv.Itoa(i)),
			})
		}
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Delete user", cbUsersDel+idStr),
	})
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func roleButton(idStr, role string, active bool) tgbotapi.InlineKeyboardButton {
	label := role
	if active {
		label = "• " + role
	}
	return tgbotapi.NewInlineKeyboardButtonData(label, cbUsersRole+idStr+":"+role)
}

func (b *Bot) userDeleteConfirmKeyboard(id int64) *tgbotapi.InlineKeyboardMarkup {
	idStr := strconv.FormatInt(id, 10)
	rows := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("Confirm delete", cbUsersDelYes+idStr),
			tgbotapi.NewInlineKeyboardButtonData("Cancel", cbUsersDelNo+idStr),
		},
		{tgbotapi.NewInlineKeyboardButtonData("« Users", cbMenuUsers)},
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func parseUserID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func parseUserIDRole(s string) (int64, acl.Role, bool) {
	idStr, roleStr, ok := strings.Cut(s, ":")
	if !ok {
		return 0, "", false
	}
	id, ok := parseUserID(idStr)
	if !ok {
		return 0, "", false
	}
	role, err := acl.ParseRole(roleStr)
	if err != nil {
		return 0, "", false
	}
	return id, role, true
}

func parseUserIDAccess(s string) (int64, acl.HostAccess, bool) {
	idStr, acc, ok := strings.Cut(s, ":")
	if !ok {
		return 0, "", false
	}
	id, ok := parseUserID(idStr)
	if !ok {
		return 0, "", false
	}
	switch acl.HostAccess(acc) {
	case acl.HostAccessAll, acl.HostAccessSelected:
		return id, acl.HostAccess(acc), true
	default:
		return 0, "", false
	}
}

func parseUserIDIdx(s string) (int64, int, bool) {
	idStr, idxStr, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, false
	}
	id, ok := parseUserID(idStr)
	if !ok {
		return 0, 0, false
	}
	idx, ok := parseIdx(idxStr)
	return id, idx, ok
}
