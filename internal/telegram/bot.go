package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"goarmmon/internal/alerts"
	"goarmmon/internal/config"
	"goarmmon/internal/state"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const connectRetryInterval = 5 * time.Second

type Bot struct {
	apiMu           sync.RWMutex
	api             *tgbotapi.BotAPI
	cfg             config.TelegramConfig
	cache           *state.Cache
	allowed         map[int64]struct{}
	notifyPartial   map[int64]bool
	stats           *connStats
	startupNotified bool
	startupCfgPath  string
	store           ConfigStore
	runCtx          context.Context
	hostEdit        *hostEditor
}

func New(cfg config.TelegramConfig, cache *state.Cache) *Bot {
	allowed := make(map[int64]struct{}, len(cfg.AllowedUsers))
	for _, id := range cfg.AllowedUsers {
		allowed[id] = struct{}{}
	}
	return &Bot{
		cfg:           cfg,
		cache:         cache,
		allowed:       allowed,
		notifyPartial: make(map[int64]bool),
		stats:         newConnStats(),
		hostEdit:      newHostEditor(),
	}
}

func (b *Bot) Run(ctx context.Context, events <-chan alerts.Event) {
	go b.connectionLoop(ctx)

	var updates tgbotapi.UpdatesChannel
	for {
		if updates == nil {
			if !b.waitConnected(ctx) {
				return
			}
			updates = b.updatesChannel()
			continue
		}

		select {
		case <-ctx.Done():
			b.stopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
				updates = nil
				b.clearAPI()
				continue
			}
			b.handleUpdate(update)
		case ev, ok := <-events:
			if !ok {
				b.stopReceivingUpdates()
				return
			}
			b.notifyEvent(ev)
		}
	}
}

func (b *Bot) waitConnected(ctx context.Context) bool {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if b.apiClient() != nil {
				return true
			}
		}
	}
}

func (b *Bot) connectionLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if b.apiClient() != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(connectRetryInterval):
			}
			continue
		}

		api, err := tgbotapi.NewBotAPI(b.cfg.Token)
		if err != nil {
			b.stats.recordFailure()
			slog.Warn("telegram connect failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(connectRetryInterval):
			}
			continue
		}

		b.setAPI(api)
		slog.Info("telegram connected", "username", api.Self.UserName)

		b.apiMu.Lock()
		notifyStartup := !b.startupNotified
		cfgPath := b.startupCfgPath
		b.apiMu.Unlock()
		if notifyStartup && cfgPath != "" {
			_, _, failed, _ := b.stats.counts()
			b.notifyStartup(cfgPath, failed)
			b.apiMu.Lock()
			b.startupNotified = true
			b.apiMu.Unlock()
		}
	}
}

func (b *Bot) SetStartupConfigPath(path string) {
	b.apiMu.Lock()
	b.startupCfgPath = path
	b.apiMu.Unlock()
}

func (b *Bot) apiClient() *tgbotapi.BotAPI {
	b.apiMu.RLock()
	defer b.apiMu.RUnlock()
	return b.api
}

func (b *Bot) setAPI(api *tgbotapi.BotAPI) {
	b.apiMu.Lock()
	b.api = api
	b.apiMu.Unlock()
}

func (b *Bot) clearAPI() {
	b.apiMu.Lock()
	b.api = nil
	b.apiMu.Unlock()
}

func (b *Bot) stopReceivingUpdates() {
	b.apiMu.RLock()
	api := b.api
	b.apiMu.RUnlock()
	if api != nil {
		api.StopReceivingUpdates()
	}
}

func (b *Bot) updatesChannel() tgbotapi.UpdatesChannel {
	api := b.apiClient()
	if api == nil {
		return nil
	}
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	return api.GetUpdatesChan(u)
}

func (b *Bot) allowedUser(id int64) bool {
	_, ok := b.allowed[id]
	return ok
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		if !b.allowedUser(update.CallbackQuery.From.ID) {
			return
		}
		b.handleCallback(update.CallbackQuery)
		return
	}
	if update.Message == nil || !update.Message.IsCommand() {
		if update.Message != nil && b.allowedUser(update.Message.From.ID) {
			if b.handleHostTextInput(update.Message) {
				return
			}
		}
		return
	}
	if !b.allowedUser(update.Message.From.ID) {
		return
	}

	cmd, args := parseCommand(update.Message.Command(), update.Message.CommandArguments())
	var text string
	var markup *tgbotapi.InlineKeyboardMarkup

	userID := update.Message.From.ID

	switch cmd {
	case "start":
		text = welcomeText()
		markup = mainMenuKeyboard()
	case "help":
		text = helpText()
		markup = mainMenuKeyboard()
	case "list":
		text = b.formatList()
		markup = actionKeyboard("list")
	case "status":
		text = b.formatStatusAll()
		markup = b.statusMenuKeyboard("")
	case "host":
		if args == "" {
			text = "Choose a host from the menu."
			markup = b.hostsMenuKeyboard()
		} else {
			text = b.formatHost(args)
			markup = b.hostDetailKeyboard(args)
		}
	case "alerts":
		text = b.formatAlerts()
		markup = actionKeyboard("alerts")
	case "uptime":
		text = b.formatUptime()
		markup = actionKeyboard("uptime")
	case "ping":
		text = b.formatPing()
		markup = actionKeyboard("ping")
	case "http":
		text = b.formatHTTP()
		markup = actionKeyboard("http")
	case "glances":
		text = b.formatGlances()
		markup = actionKeyboard("glances")
	case "stats":
		text = b.formatStats()
		markup = actionKeyboard("stats")
	case "notify_partial":
		text = settingsText(b.setNotifyPartial(userID, args))
		markup = b.settingsMenuKeyboard(userID)
	case "cancel":
		text = b.cancelHostEdit(userID)
		markup = mainMenuKeyboard()
	default:
		text = "Unknown command. Use /help."
		markup = mainMenuKeyboard()
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if markup != nil {
		msg.ReplyMarkup = markup
	}
	if _, err := b.send(msg); err != nil {
		slog.Warn("telegram send failed", "error", err)
	}
}

func (b *Bot) handleCallback(q *tgbotapi.CallbackQuery) {
	data := q.Data
	userID := q.From.ID
	var text string
	var markup *tgbotapi.InlineKeyboardMarkup

	switch {
	case data == cbMenuMain:
		text = menuTitle("main")
		markup = mainMenuKeyboard()
	case data == cbMenuStatus:
		text = b.formatStatusAll()
		markup = b.statusMenuKeyboard("")
	case data == cbMenuHosts:
		text = menuTitle("hosts")
		markup = b.hostsMenuKeyboard()
	case data == cbMenuChecks:
		text = menuTitle("checks")
		markup = checksMenuKeyboard()
	case data == cbMenuSettings:
		text = settingsText("")
		markup = b.settingsMenuKeyboard(userID)
	case data == cbActionList:
		text = b.formatList()
		markup = actionKeyboard("list")
	case data == cbActionAlerts:
		text = b.formatAlerts()
		markup = actionKeyboard("alerts")
	case data == cbActionPing:
		text = b.formatPing()
		markup = actionKeyboard("ping")
	case data == cbActionHTTP:
		text = b.formatHTTP()
		markup = actionKeyboard("http")
	case data == cbActionGlances:
		text = b.formatGlances()
		markup = actionKeyboard("glances")
	case data == cbActionUptime:
		text = b.formatUptime()
		markup = actionKeyboard("uptime")
	case data == cbActionStats:
		text = b.formatStats()
		markup = actionKeyboard("stats")
	case data == cbActionHelp:
		text = helpText()
		markup = mainMenuKeyboard()
	case strings.HasPrefix(data, cbNotifyPartial):
		arg := strings.TrimPrefix(data, cbNotifyPartial)
		text = settingsText(b.setNotifyPartial(userID, arg))
		markup = b.settingsMenuKeyboard(userID)
	case data == cbStatusAll:
		text = b.formatStatusAll()
		markup = b.statusMenuKeyboard("")
	case strings.HasPrefix(data, cbStatusGroup):
		group := strings.TrimPrefix(data, cbStatusGroup)
		text = b.formatStatusGroup(group)
		markup = b.statusMenuKeyboard(group)
	case strings.HasPrefix(data, cbStatusHost):
		host := strings.TrimPrefix(data, cbStatusHost)
		text = b.formatHost(host)
		markup = b.hostDetailKeyboard(host)
	default:
		var handled bool
		text, markup, handled = b.handleHostCallback(data, userID, q.Message.Chat.ID, q.Message.MessageID)
		if !handled {
			text = "Unknown action"
			markup = mainMenuKeyboard()
		}
	}

	edit := tgbotapi.NewEditMessageText(q.Message.Chat.ID, q.Message.MessageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	if markup != nil {
		edit.ReplyMarkup = markup
	}
	if _, err := b.send(edit); err != nil {
		slog.Warn("telegram edit failed", "error", err)
	}

	callback := tgbotapi.NewCallback(q.ID, "")
	if _, err := b.request(callback); err != nil {
		slog.Warn("telegram callback ack failed", "error", err)
	}
}

func (b *Bot) notifyStartup(cfgPath string, failedAttempts int) {
	hostCount := len(b.cache.ListHosts())
	text := fmt.Sprintf(
		"<b>STARTUP</b>\nMonitor service started\nConfig: %s\nHosts: %d\nFailed connect attempts: %d\n%s",
		escapeHTML(cfgPath),
		hostCount,
		failedAttempts,
		time.Now().Format(time.RFC3339),
	)
	b.sendToAll(text)
}

func (b *Bot) send(ch tgbotapi.Chattable) (tgbotapi.Message, error) {
	api := b.apiClient()
	if api == nil {
		return tgbotapi.Message{}, fmt.Errorf("telegram not connected")
	}
	return api.Send(ch)
}

func (b *Bot) request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	api := b.apiClient()
	if api == nil {
		return nil, fmt.Errorf("telegram not connected")
	}
	return api.Request(c)
}

func (b *Bot) notifyEvent(ev alerts.Event) {
	text := fmt.Sprintf("<b>%s</b>\n%s\n%s", strings.ToUpper(string(ev.Kind)), ev.Message, ev.At.Format(time.RFC3339))
	if ev.Kind == alerts.EventPartial {
		b.sendPartialToSubscribers(text)
		return
	}
	b.sendToAll(text)
}

func (b *Bot) sendPartialToSubscribers(text string) {
	for id := range b.allowed {
		if !b.notifyPartial[id] {
			continue
		}
		msg := tgbotapi.NewMessage(id, text)
		msg.ParseMode = tgbotapi.ModeHTML
		if _, err := b.send(msg); err != nil {
			slog.Warn("telegram notify failed", "user", id, "error", err)
		}
	}
}

func (b *Bot) sendToAll(text string) {
	for id := range b.allowed {
		msg := tgbotapi.NewMessage(id, text)
		msg.ParseMode = tgbotapi.ModeHTML
		if _, err := b.send(msg); err != nil {
			slog.Warn("telegram notify failed", "user", id, "error", err)
		}
	}
}

func parseCommand(command, args string) (string, string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return command, ""
	}
	return command, parts[0]
}

func helpText() string {
	return strings.Join([]string{
		"<b>Commands</b>",
		"Use the inline buttons or these commands:",
		"/start - main menu",
		"/help - this message",
		"/list - all hosts",
		"/status - status summary",
		"/host &lt;name&gt; - host details",
		"/alerts - active alerts",
		"/uptime - glances uptime",
		"/ping - ping RTT",
		"/http - http check results",
		"/glances - glances summary",
		"/stats - telegram API stats",
		"/notify_partial [on|off] - toggle PARTIAL ping notifications (off by default)",
		"",
		"<b>Host management</b>",
		"Hosts menu → Add host / Manage",
		"/cancel - cancel current host edit",
	}, "\n")
}

func (b *Bot) formatList() string {
	hosts := b.cache.ListHosts()
	if len(hosts) == 0 {
		return "No hosts configured."
	}
	var sb strings.Builder
	sb.WriteString("<b>Hosts</b>\n")
	for _, h := range hosts {
		group := h.Group
		if group == "" {
			group = "-"
		}
		sb.WriteString(fmt.Sprintf("• <b>%s</b> [%s] — %s\n", h.Name, group, h.StatusLabel()))
		if h.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", escapeHTML(h.Description)))
		}
	}
	return sb.String()
}

func (b *Bot) formatStatusAll() string {
	return b.formatStatusHosts(b.cache.ListHosts(), "All hosts")
}

func (b *Bot) formatStatusGroup(group string) string {
	return b.formatStatusHosts(b.cache.HostsByGroup(group), "Group: "+group)
}

func (b *Bot) formatStatusHosts(hosts []state.HostView, title string) string {
	if len(hosts) == 0 {
		return "No hosts found."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n", escapeHTML(title)))
	for _, h := range hosts {
		sb.WriteString(fmt.Sprintf("• <b>%s</b>: %s\n", h.Name, h.StatusLabel()))
	}
	return sb.String()
}

func (b *Bot) formatHost(name string) string {
	h, ok := b.cache.GetHost(name)
	if !ok {
		return fmt.Sprintf("Host %q not found.", name)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b> — %s\n", h.Name, h.StatusLabel()))
	if h.Description != "" {
		sb.WriteString(escapeHTML(h.Description) + "\n")
	}
	if h.Group != "" {
		sb.WriteString(fmt.Sprintf("Group: %s\n", escapeHTML(h.Group)))
	}
	for _, ch := range h.Checks {
		sb.WriteString(fmt.Sprintf("\n<b>%s</b>: %s\n", ch.CheckType, ch.StatusLabel()))
		if ch.Error != "" {
			sb.WriteString("Error: " + escapeHTML(ch.Error) + "\n")
		}
		switch ch.CheckType {
		case "ping":
			sb.WriteString(fmt.Sprintf("RTT: %s\n", ch.RTT.Round(time.Millisecond)))
		case "http":
			sb.WriteString(fmt.Sprintf("Code: %d, time: %s\n", ch.HTTPCode, ch.HTTPDuration.Round(time.Millisecond)))
		case "glances":
			if ch.Glances != nil {
				g := ch.Glances
				sb.WriteString(fmt.Sprintf("CPU: %.1f%%, RAM: %.1f%%, Swap: %.1f%%\n", g.CPU, g.RAM, g.Swap))
				sb.WriteString(fmt.Sprintf("Load: %.2f / %.2f / %.2f\n", g.Load[0], g.Load[1], g.Load[2]))
				sb.WriteString(fmt.Sprintf("Uptime: %s\n", g.Uptime.Round(time.Second)))
				sb.WriteString(fmt.Sprintf("Processes: %d\n", g.ProcessCount))
				for _, fs := range g.Filesystems {
					line := fmt.Sprintf("Disk %s (%s): %.1f%%", fs.Device, fs.Mount, fs.UsedPct)
					if fs.Ignored {
						line += " [ignored]"
					}
					sb.WriteString(line + "\n")
				}
			}
		}
	}
	return sb.String()
}

func (b *Bot) formatAlerts() string {
	alerts := b.cache.ListAlerts()
	if len(alerts) == 0 {
		return "No active alerts."
	}
	var sb strings.Builder
	sb.WriteString("<b>Active alerts</b>\n")
	for _, a := range alerts {
		sb.WriteString(fmt.Sprintf("• %s/%s: %s\n", a.HostName, a.CheckType, a.Status))
		if a.Error != "" {
			sb.WriteString("  " + escapeHTML(a.Error) + "\n")
		}
	}
	return sb.String()
}

func (b *Bot) formatUptime() string {
	snaps := b.cache.AllGlances()
	if len(snaps) == 0 {
		return "No glances data."
	}
	var sb strings.Builder
	sb.WriteString("<b>Uptime</b>\n")
	for _, s := range snaps {
		if s.Glances != nil {
			sb.WriteString(fmt.Sprintf("• %s: %s\n", s.HostName, s.Glances.Uptime.Round(time.Second)))
		}
	}
	return sb.String()
}

func (b *Bot) formatPing() string {
	snaps := b.cache.AllPing()
	if len(snaps) == 0 {
		return "No ping checks."
	}
	var sb strings.Builder
	sb.WriteString("<b>Ping</b>\n")
	for _, s := range snaps {
		sb.WriteString(fmt.Sprintf("• %s: %s", s.HostName, s.StatusLabel()))
		if s.Status == state.StatusOnline {
			sb.WriteString(fmt.Sprintf(" (%s)", s.RTT.Round(time.Millisecond)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (b *Bot) formatHTTP() string {
	snaps := b.cache.AllHTTP()
	if len(snaps) == 0 {
		return "No HTTP checks."
	}
	var sb strings.Builder
	sb.WriteString("<b>HTTP</b>\n")
	for _, s := range snaps {
		sb.WriteString(fmt.Sprintf("• %s: %s code %d (%s)\n", s.HostName, s.Status, s.HTTPCode, s.HTTPDuration.Round(time.Millisecond)))
	}
	return sb.String()
}

func (b *Bot) formatGlances() string {
	snaps := b.cache.AllGlances()
	if len(snaps) == 0 {
		return "No glances checks."
	}
	var sb strings.Builder
	sb.WriteString("<b>Glances</b>\n")
	for _, s := range snaps {
		if s.Glances == nil {
			sb.WriteString(fmt.Sprintf("• %s: %s\n", s.HostName, s.Status))
			continue
		}
		g := s.Glances
		sb.WriteString(fmt.Sprintf("• %s: CPU %.1f%%, RAM %.1f%%, Swap %.1f%%\n", s.HostName, g.CPU, g.RAM, g.Swap))
	}
	return sb.String()
}

func (b *Bot) formatStats() string {
	hour, day, total, startedAt := b.stats.counts()
	uptime := time.Since(startedAt).Round(time.Second)

	var sb strings.Builder
	sb.WriteString("<b>Telegram API stats</b>\n")

	api := b.apiClient()
	if api == nil {
		sb.WriteString("Status: <b>disconnected</b>\n")
		sb.WriteString("Ping: n/a\n")
	} else {
		sb.WriteString("Status: <b>connected</b>\n")
		start := time.Now()
		if _, err := api.GetMe(); err != nil {
			sb.WriteString(fmt.Sprintf("Ping: error (%s)\n", escapeHTML(err.Error())))
		} else {
			sb.WriteString(fmt.Sprintf("Ping: %s\n", time.Since(start).Round(time.Millisecond)))
		}
	}

	sb.WriteString(fmt.Sprintf("Failed attempts (1h): %d\n", hour))
	sb.WriteString(fmt.Sprintf("Failed attempts (24h): %d\n", day))
	sb.WriteString(fmt.Sprintf("Failed attempts (total): %d\n", total))
	sb.WriteString(fmt.Sprintf("Monitor uptime: %s\n", uptime))
	return sb.String()
}

func (b *Bot) setNotifyPartial(userID int64, arg string) string {
	arg = strings.ToLower(strings.TrimSpace(arg))
	switch arg {
	case "":
		if b.notifyPartial[userID] {
			return "PARTIAL ping notifications: <b>on</b>\nUse /notify_partial off to disable."
		}
		return "PARTIAL ping notifications: <b>off</b> (default)\nUse /notify_partial on to enable."
	case "on", "enable", "1", "true":
		b.notifyPartial[userID] = true
		return "PARTIAL ping notifications: <b>on</b>"
	case "off", "disable", "0", "false":
		b.notifyPartial[userID] = false
		return "PARTIAL ping notifications: <b>off</b>"
	case "toggle":
		b.notifyPartial[userID] = !b.notifyPartial[userID]
		if b.notifyPartial[userID] {
			return "PARTIAL ping notifications: <b>on</b>"
		}
		return "PARTIAL ping notifications: <b>off</b>"
	default:
		return "Usage: /notify_partial [on|off|toggle]"
	}
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func (b *Bot) UpdateConfig(cfg config.TelegramConfig) {
	b.cfg = cfg
	allowed := make(map[int64]struct{}, len(cfg.AllowedUsers))
	for _, id := range cfg.AllowedUsers {
		allowed[id] = struct{}{}
	}
	b.allowed = allowed
}
