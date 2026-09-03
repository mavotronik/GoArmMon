package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"goarmmon/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cbMenuMain     = "menu:main"
	cbMenuStatus   = "menu:status"
	cbMenuHosts    = "menu:hosts"
	cbMenuChecks   = "menu:checks"
	cbMenuSettings = "menu:settings"

	cbActionList    = "action:list"
	cbActionAlerts  = "action:alerts"
	cbActionPing    = "action:ping"
	cbActionHTTP    = "action:http"
	cbActionGlances = "action:glances"
	cbActionUptime  = "action:uptime"
	cbActionStats   = "action:stats"
	cbActionHelp    = "action:help"

	cbStatusAll   = "status:all"
	cbStatusGroup = "status:group:"
	cbStatusHost  = "status:host:"

	cbNotifyPartial = "action:notify_partial:"
)

func mainMenuKeyboard() *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("Status", cbMenuStatus),
			tgbotapi.NewInlineKeyboardButtonData("Hosts", cbMenuHosts),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Checks", cbMenuChecks),
			tgbotapi.NewInlineKeyboardButtonData("Alerts", cbActionAlerts),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Settings", cbMenuSettings),
			tgbotapi.NewInlineKeyboardButtonData("Help", cbActionHelp),
		},
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func backToMenuRow() []tgbotapi.InlineKeyboardButton {
	return []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("« Menu", cbMenuMain),
	}
}

func (b *Bot) statusMenuKeyboard(userID int64, activeGroup string) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{backToMenuRow()}

	allLabel := "All"
	if activeGroup == "" {
		allLabel = "• All"
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(allLabel, cbStatusAll),
	})

	for _, g := range b.visibleGroups(userID) {
		label := "Group: " + g
		if g == activeGroup {
			label = "• " + label
		}
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(label, cbStatusGroup+g),
		})
	}

	hosts := b.visibleHosts(userID)
	for i := 0; i < len(hosts); i += 2 {
		row := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(hosts[i].Name, cbStatusHost+hosts[i].Name),
		}
		if i+1 < len(hosts) {
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(hosts[i+1].Name, cbStatusHost+hosts[i+1].Name))
		}
		rows = append(rows, row)
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostsMenuKeyboard(userID int64) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{backToMenuRow()}
	if b.canAddHost(userID) {
		manageRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Add host", cbHostAdd),
		}
		if b.hasEditableHosts(userID) {
			manageRow = append(manageRow, tgbotapi.NewInlineKeyboardButtonData("Manage", cbHostManage))
		}
		rows = append(rows, manageRow)
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("All hosts", cbActionList),
	})

	hosts := b.visibleHosts(userID)
	for i := 0; i < len(hosts); i += 2 {
		row := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(hosts[i].Name, cbStatusHost+hosts[i].Name),
		}
		if i+1 < len(hosts) {
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(hosts[i+1].Name, cbStatusHost+hosts[i+1].Name))
		}
		rows = append(rows, row)
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hasEditableHosts(userID int64) bool {
	if b.store == nil {
		return false
	}
	for _, h := range b.store.HostConfigs() {
		if b.canEditHost(userID, h.Name) {
			return true
		}
	}
	return false
}

func checksMenuKeyboard() *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{
			tgbotapi.NewInlineKeyboardButtonData("Ping", cbActionPing),
			tgbotapi.NewInlineKeyboardButtonData("HTTP", cbActionHTTP),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Glances", cbActionGlances),
			tgbotapi.NewInlineKeyboardButtonData("Uptime", cbActionUptime),
		},
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) settingsMenuKeyboard(userID int64) *tgbotapi.InlineKeyboardMarkup {
	partialLabel := "PARTIAL notify: off"
	if b.notifyPartial[userID] {
		partialLabel = "PARTIAL notify: on"
	}

	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("API stats", cbActionStats)},
		{tgbotapi.NewInlineKeyboardButtonData(partialLabel, cbNotifyPartial+"toggle")},
		{
			tgbotapi.NewInlineKeyboardButtonData("Turn on", cbNotifyPartial+"on"),
			tgbotapi.NewInlineKeyboardButtonData("Turn off", cbNotifyPartial+"off"),
		},
	}
	if b.canManageUsers(userID) {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Users", cbMenuUsers),
		})
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func menuTitle(category string) string {
	switch category {
	case "main":
		return "<b>Menu</b>\nChoose a category:"
	case "status":
		return "<b>Status</b>\nChoose scope:"
	case "hosts":
		return "<b>Hosts</b>\nChoose a host or view all:"
	case "checks":
		return "<b>Checks</b>\nChoose check type:"
	case "settings":
		return "<b>Settings</b>"
	default:
		return ""
	}
}

func hostDetailKeyboardWithEdit(hostName string, idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("« Hosts", cbMenuHosts)},
		{tgbotapi.NewInlineKeyboardButtonData("« Status", cbMenuStatus)},
	}
	if hostName != "" {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Refresh", cbStatusHost+hostName),
		})
	}
	if idx >= 0 {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Edit", cbHostEdit+strconv.Itoa(idx)),
		})
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostDetailKeyboard(userID int64, hostName string) *tgbotapi.InlineKeyboardMarkup {
	idx := -1
	if b.canEditHost(userID, hostName) {
		if i, ok := hostIndexByName(b.store, hostName); ok {
			idx = i
		}
	}
	return hostDetailKeyboardWithEdit(hostName, idx)
}

func (b *Bot) hostManageListKeyboard(userID int64) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("« Hosts", cbMenuHosts)},
	}
	if b.canAddHost(userID) {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Add host", cbHostAdd),
		})
	}
	if b.store != nil {
		hosts := b.store.HostConfigs()
		for i, h := range hosts {
			if !b.canEditHost(userID, h.Name) {
				continue
			}
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("Edit: "+h.Name, cbHostEdit+strconv.Itoa(i)),
			})
		}
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func hostBackToHostRow(idx int) []tgbotapi.InlineKeyboardButton {
	return []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbHostEdit+strconv.Itoa(idx)),
	}
}

func (b *Bot) hostManageCardKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("« Manage", cbHostManage)},
		{
			tgbotapi.NewInlineKeyboardButtonData("General", cbHostGeneral+strconv.Itoa(idx)+":menu"),
			tgbotapi.NewInlineKeyboardButtonData("Checks", cbHostCheck+strconv.Itoa(idx)+":menu"),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Alerts", cbHostAlert+strconv.Itoa(idx)+":menu"),
			tgbotapi.NewInlineKeyboardButtonData("Messages", cbHostMsg+strconv.Itoa(idx)+":menu"),
		},
		{tgbotapi.NewInlineKeyboardButtonData("Delete host", cbHostDel+strconv.Itoa(idx))},
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostGeneralMenuKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{hostBackToHostRow(idx)}
	rows = append(rows, b.hostGeneralRow(idx)...)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostChecksMenuKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{hostBackToHostRow(idx)}
	rows = append(rows, b.hostChecksRow(idx)...)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostAlertsMenuKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{hostBackToHostRow(idx)}
	rows = append(rows, b.hostAlertsRow(idx)...)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostMessagesMenuKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{hostBackToHostRow(idx)}
	rows = append(rows, b.hostMessagesRow(idx)...)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostGeneralRow(idx int) [][]tgbotapi.InlineKeyboardButton {
	prefix := cbHostField + strconv.Itoa(idx) + ":"
	skipLabel := "skip_on_ping: off"
	hosts := b.store.HostConfigs()
	if idx >= 0 && idx < len(hosts) && hosts[idx].SkipOnPingFailure {
		skipLabel = "skip_on_ping: on"
	}
	return [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("Name", prefix+"name"),
			tgbotapi.NewInlineKeyboardButtonData("Description", prefix+"description"),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Group", prefix+"group"),
			tgbotapi.NewInlineKeyboardButtonData(skipLabel, cbHostToggle+strconv.Itoa(idx)+":skip_on_ping_failure"),
		},
	}
}

func (b *Bot) hostChecksRow(idx int) [][]tgbotapi.InlineKeyboardButton {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return nil
	}
	h := hosts[idx]
	types := config.ListCheckTypes(h)
	known := map[string]bool{"ping": false, "http": false, "glances": false}
	for _, t := range types {
		known[t] = true
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, checkType := range []string{"ping", "http", "glances"} {
		if known[checkType] {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(checkType, cbHostCheck+strconv.Itoa(idx)+":"+checkType),
			})
		} else {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("+ "+checkType, cbHostCheckAdd+strconv.Itoa(idx)+":"+checkType),
			})
		}
	}
	return rows
}

func (b *Bot) hostAlertsRow(idx int) [][]tgbotapi.InlineKeyboardButton {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return nil
	}
	h := hosts[idx]
	prefix := cbHostAlert + strconv.Itoa(idx) + ":"

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Default for", cbHostField+strconv.Itoa(idx)+":alerts.for"),
	})

	var buttons []tgbotapi.InlineKeyboardButton
	if hostAlertApplicable(h, "rtt") {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("RTT", prefix+"rtt"))
	}
	if hostAlertApplicable(h, "http_response") {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("HTTP", prefix+"http_response"))
	}
	if hostAlertApplicable(h, "cpu") {
		buttons = append(buttons,
			tgbotapi.NewInlineKeyboardButtonData("CPU", prefix+"cpu"),
			tgbotapi.NewInlineKeyboardButtonData("RAM", prefix+"ram"),
			tgbotapi.NewInlineKeyboardButtonData("Swap", prefix+"swap"),
			tgbotapi.NewInlineKeyboardButtonData("Disk", prefix+"disk"),
		)
	}
	for i := 0; i < len(buttons); i += 2 {
		row := []tgbotapi.InlineKeyboardButton{buttons[i]}
		if i+1 < len(buttons) {
			row = append(row, buttons[i+1])
		}
		rows = append(rows, row)
	}
	return rows
}

func (b *Bot) hostMessagesRow(idx int) [][]tgbotapi.InlineKeyboardButton {
	prefix := cbHostField + strconv.Itoa(idx) + ":messages."
	return [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("Offline", prefix+"offline"),
			tgbotapi.NewInlineKeyboardButtonData("Online", prefix+"online"),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Warning", prefix+"warning"),
			tgbotapi.NewInlineKeyboardButtonData("Critical", prefix+"critical"),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData("Recovery", prefix+"recovery"),
		},
	}
}

func (b *Bot) hostCheckKeyboard(idx int, checkType string) *tgbotapi.InlineKeyboardMarkup {
	prefix := cbHostField + strconv.Itoa(idx) + ":checks." + checkType + "."
	var fieldRows [][]tgbotapi.InlineKeyboardButton
	switch checkType {
	case "ping":
		fieldRows = [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("Address", prefix+"address")},
			{
				tgbotapi.NewInlineKeyboardButtonData("Timeout", prefix+"timeout"),
				tgbotapi.NewInlineKeyboardButtonData("Interval", prefix+"interval"),
			},
			{tgbotapi.NewInlineKeyboardButtonData("Fail threshold", prefix+"fail_threshold")},
		}
	case "http":
		hosts := b.store.HostConfigs()
		followLabel := "follow_redirects: off"
		if idx >= 0 && idx < len(hosts) {
			if cfg, _, ok := config.GetHTTPCheck(&hosts[idx]); ok && cfg.FollowRedirects {
				followLabel = "follow_redirects: on"
			}
		}
		fieldRows = [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("URL", prefix+"url")},
			{
				tgbotapi.NewInlineKeyboardButtonData("Method", prefix+"method"),
				tgbotapi.NewInlineKeyboardButtonData("Expected code", prefix+"expected_code"),
			},
			{
				tgbotapi.NewInlineKeyboardButtonData("Timeout", prefix+"timeout"),
				tgbotapi.NewInlineKeyboardButtonData("Interval", prefix+"interval"),
			},
			{tgbotapi.NewInlineKeyboardButtonData(followLabel, cbHostToggle+strconv.Itoa(idx)+":checks.http.follow_redirects")},
		}
	case "glances":
		fieldRows = [][]tgbotapi.InlineKeyboardButton{
			{tgbotapi.NewInlineKeyboardButtonData("URL", prefix+"url")},
			{tgbotapi.NewInlineKeyboardButtonData("API version", prefix+"api_version")},
			{
				tgbotapi.NewInlineKeyboardButtonData("Timeout", prefix+"timeout"),
				tgbotapi.NewInlineKeyboardButtonData("Interval", prefix+"interval"),
			},
			{
				tgbotapi.NewInlineKeyboardButtonData("Username", prefix+"username"),
				tgbotapi.NewInlineKeyboardButtonData("Password", prefix+"password"),
			},
			{tgbotapi.NewInlineKeyboardButtonData("Token", prefix+"token")},
		}
	}
	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("« Back", cbHostCheck+strconv.Itoa(idx)+":menu")},
		{tgbotapi.NewInlineKeyboardButtonData("Remove check", cbHostCheckRm+strconv.Itoa(idx)+":"+checkType)},
	}
	rows = append(rows, fieldRows...)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostAlertKeyboard(idx int, alertKey string) *tgbotapi.InlineKeyboardMarkup {
	fldPrefix := cbHostAlertFld + strconv.Itoa(idx) + ":" + alertKey + ":"
	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbHostAlert+strconv.Itoa(idx)+":menu"),
		tgbotapi.NewInlineKeyboardButtonData("Disable", cbHostAlertDis+strconv.Itoa(idx)+":"+alertKey),
	})
	if alertKey != "menu" {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Warning", fldPrefix+"warning"),
			tgbotapi.NewInlineKeyboardButtonData("Critical", fldPrefix+"critical"),
		})
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Recovery", fldPrefix+"recovery"),
			tgbotapi.NewInlineKeyboardButtonData("For", fldPrefix+"for"),
		})
	}
	if alertKey == "disk" {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Ignore devices", cbHostField+strconv.Itoa(idx)+":alerts.disk.ignore_devices"),
			tgbotapi.NewInlineKeyboardButtonData("Ignore mounts", cbHostField+strconv.Itoa(idx)+":alerts.disk.ignore_mounts"),
		})
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) hostDeleteConfirmKeyboard(idx int) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{
			tgbotapi.NewInlineKeyboardButtonData("Confirm delete", cbHostDelYes+strconv.Itoa(idx)),
			tgbotapi.NewInlineKeyboardButtonData("Cancel", cbHostDelNo+strconv.Itoa(idx)),
		},
		{tgbotapi.NewInlineKeyboardButtonData("« Back", cbHostEdit+strconv.Itoa(idx))},
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func actionKeyboard(action string) *tgbotapi.InlineKeyboardMarkup {
	var backRow []tgbotapi.InlineKeyboardButton
	switch action {
	case "ping", "http", "glances", "uptime":
		backRow = []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("« Checks", cbMenuChecks),
			tgbotapi.NewInlineKeyboardButtonData("« Menu", cbMenuMain),
		}
	default:
		backRow = backToMenuRow()
	}
	rows := [][]tgbotapi.InlineKeyboardButton{backRow}
	if action != "" {
		var refreshData string
		switch action {
		case "list":
			refreshData = cbActionList
		case "alerts":
			refreshData = cbActionAlerts
		case "ping":
			refreshData = cbActionPing
		case "http":
			refreshData = cbActionHTTP
		case "glances":
			refreshData = cbActionGlances
		case "uptime":
			refreshData = cbActionUptime
		case "stats":
			refreshData = cbActionStats
		case "help":
			refreshData = cbActionHelp
		}
		if refreshData != "" {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("Refresh", refreshData),
			})
		}
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func welcomeText() string {
	return strings.Join([]string{
		"<b>Glances Telegram Monitor</b>",
		"",
		"Use the buttons below or /help for commands.",
	}, "\n")
}

func settingsText(notifyMsg string) string {
	if notifyMsg != "" {
		return fmt.Sprintf("<b>Settings</b>\n\n%s", notifyMsg)
	}
	return menuTitle("settings")
}
