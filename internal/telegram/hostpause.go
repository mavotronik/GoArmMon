package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"goarmmon/internal/hostpause"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cbHostPauseMenu = "hpause:menu:"
	cbHostPauseSet  = "hpause:set:"
	cbHostPauseClr  = "hpause:clr:"
)

var pausePresetLabels = []struct {
	preset string
	label  string
}{
	{hostpause.Preset5m, "5 min"},
	{hostpause.Preset15m, "15 min"},
	{hostpause.Preset3h, "3 hours"},
	{hostpause.Preset12h, "12 hours"},
	{hostpause.Preset24h, "24 hours"},
	{hostpause.PresetForever, "Forever"},
}

func (b *Bot) SetPauses(store *hostpause.Store) {
	b.pauses = store
}

func (b *Bot) handleHostPauseCallback(data string, userID int64) (text string, markup *tgbotapi.InlineKeyboardMarkup, handled bool) {
	if b.pauses == nil || b.store == nil {
		return "", nil, false
	}

	switch {
	case strings.HasPrefix(data, cbHostPauseMenu):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostPauseMenu))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		name, ok := hostNameByIndex(b.store, idx)
		if !ok || !b.canViewHost(userID, name) {
			return "Host not found.", mainMenuKeyboard(), true
		}
		if !b.canEditHost(userID, name) {
			b.logDenied(userID, "hpause:menu", name)
			return "Access denied.", b.hostDetailKeyboard(userID, name), true
		}
		return b.formatHostPauseMenu(idx), b.hostPauseMenuKeyboard(idx, name), true

	case strings.HasPrefix(data, cbHostPauseSet):
		rest := strings.TrimPrefix(data, cbHostPauseSet)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		if text, markup, denied := b.denyUnlessCanEditIdx(userID, idx); denied {
			return text, markup, true
		}
		name, ok := hostNameByIndex(b.store, idx)
		if !ok {
			return "Host not found.", mainMenuKeyboard(), true
		}
		preset := parts[1]
		if preset == "inf" {
			preset = hostpause.PresetForever
		}
		if err := b.pauses.SetFromPreset(name, preset); err != nil {
			return "Failed to pause checks: " + escapeHTML(err.Error()), b.hostPauseMenuKeyboard(idx, name), true
		}
		return b.formatHost(userID, name), b.hostDetailKeyboard(userID, name), true

	case strings.HasPrefix(data, cbHostPauseClr):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostPauseClr))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		if text, markup, denied := b.denyUnlessCanEditIdx(userID, idx); denied {
			return text, markup, true
		}
		name, ok := hostNameByIndex(b.store, idx)
		if !ok {
			return "Host not found.", mainMenuKeyboard(), true
		}
		if err := b.pauses.Clear(name); err != nil {
			return "Failed to enable checks: " + escapeHTML(err.Error()), b.hostPauseMenuKeyboard(idx, name), true
		}
		return b.formatHost(userID, name), b.hostDetailKeyboard(userID, name), true
	}

	return "", nil, false
}

func (b *Bot) formatHostPauseMenu(idx int) string {
	name, ok := hostNameByIndex(b.store, idx)
	if !ok {
		return "Host not found."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Checks pause</b> — %s\n\n", escapeHTML(name)))
	if b.pauses != nil {
		if p, paused := b.pauses.Get(name); paused {
			sb.WriteString("Status: ")
			sb.WriteString(formatPauseStatusText(p))
			sb.WriteString("\n\nChoose how long to disable checks:")
		} else {
			sb.WriteString("Checks are currently <b>enabled</b>.\n\nChoose how long to disable checks:")
		}
	}
	return sb.String()
}

func (b *Bot) hostPauseMenuKeyboard(idx int, hostName string) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		backToMenuRow(),
		{tgbotapi.NewInlineKeyboardButtonData("« Host", cbStatusHost+hostName)},
	}

	idxStr := strconv.Itoa(idx)
	for i := 0; i < len(pausePresetLabels); i += 2 {
		row := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				pausePresetLabels[i].label,
				cbHostPauseSet+idxStr+":"+pausePresetLabels[i].preset,
			),
		}
		if i+1 < len(pausePresetLabels) {
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(
				pausePresetLabels[i+1].label,
				cbHostPauseSet+idxStr+":"+pausePresetLabels[i+1].preset,
			))
		}
		rows = append(rows, row)
	}

	if b.pauses != nil {
		if _, paused := b.pauses.Get(hostName); paused {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("Enable checks", cbHostPauseClr+idxStr),
			})
		}
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func (b *Bot) pauseButtonLabel(hostName string) string {
	if b.pauses == nil {
		return "Checks: ON"
	}
	if p, ok := b.pauses.Get(hostName); ok {
		return "Checks: OFF · " + formatPauseRemaining(p)
	}
	return "Checks: ON"
}

func (b *Bot) formatHostChecksPauseLine(hostName string) string {
	if b.pauses == nil {
		return "Checks: active"
	}
	if p, ok := b.pauses.Get(hostName); ok {
		return "Checks: " + formatPauseStatusText(p)
	}
	return "Checks: active"
}

func formatPauseStatusText(p hostpause.Pause) string {
	if p.Until == nil {
		return "paused forever"
	}
	return "paused · " + formatPauseRemaining(p) + " left"
}

func formatPauseRemaining(p hostpause.Pause) string {
	if p.Until == nil {
		return "forever"
	}
	left := time.Until(*p.Until)
	if left <= 0 {
		return "0m"
	}
	if left < time.Hour {
		mins := int(left.Round(time.Minute) / time.Minute)
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("%dm", mins)
	}
	if left < 24*time.Hour {
		hours := int(left.Round(time.Hour) / time.Hour)
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}
	days := int(left.Round(24*time.Hour) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	return fmt.Sprintf("%dd", days)
}

func hostNameByIndex(store ConfigStore, idx int) (string, bool) {
	if store == nil {
		return "", false
	}
	hosts := store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "", false
	}
	return hosts[idx].Name, true
}
