package telegram

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cbMenuMain     = "menu:main"
	cbMenuStatus   = "menu:status"
	cbMenuHosts    = "menu:hosts"
	cbMenuChecks   = "menu:checks"
	cbMenuSettings = "menu:settings"

	cbActionList   = "action:list"
	cbActionAlerts = "action:alerts"
	cbActionPing   = "action:ping"
	cbActionHTTP   = "action:http"
	cbActionGlances = "action:glances"
	cbActionUptime = "action:uptime"
	cbActionStats  = "action:stats"
	cbActionHelp   = "action:help"

	cbStatusAll    = "status:all"
	cbStatusGroup  = "status:group:"
	cbStatusHost   = "status:host:"

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

func (b *Bot) statusMenuKeyboard(activeGroup string) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{backToMenuRow()}

	allLabel := "All"
	if activeGroup == "" {
		allLabel = "• All"
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(allLabel, cbStatusAll),
	})

	for _, g := range b.cache.Groups() {
		label := "Group: " + g
		if g == activeGroup {
			label = "• " + label
		}
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(label, cbStatusGroup+g),
		})
	}

	hosts := b.cache.ListHosts()
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

func (b *Bot) hostsMenuKeyboard() *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("All hosts", cbActionList)},
	}

	hosts := b.cache.ListHosts()
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

func hostDetailKeyboard(hostName string) *tgbotapi.InlineKeyboardMarkup {
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
