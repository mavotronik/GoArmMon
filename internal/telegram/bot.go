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
	stats           *connStats
	startupNotified bool
	startupCfgPath  string
}

func New(cfg config.TelegramConfig, cache *state.Cache) *Bot {
	allowed := make(map[int64]struct{}, len(cfg.AllowedUsers))
	for _, id := range cfg.AllowedUsers {
		allowed[id] = struct{}{}
	}
	return &Bot{
		cfg:     cfg,
		cache:   cache,
		allowed: allowed,
		stats:   newConnStats(),
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
		return
	}
	if !b.allowedUser(update.Message.From.ID) {
		return
	}

	cmd, args := parseCommand(update.Message.Command(), update.Message.CommandArguments())
	var text string
	var markup *tgbotapi.InlineKeyboardMarkup

	switch cmd {
	case "start":
		text = "Glances Telegram Monitor\n\nUse /help for commands."
	case "help":
		text = helpText()
	case "list":
		text = b.formatList()
	case "status":
		text = b.formatStatusAll()
		markup = b.statusKeyboard("")
	case "host":
		if args == "" {
			text = "Usage: /host <name>"
		} else {
			text = b.formatHost(args)
		}
	case "alerts":
		text = b.formatAlerts()
	case "uptime":
		text = b.formatUptime()
	case "ping":
		text = b.formatPing()
	case "http":
		text = b.formatHTTP()
	case "glances":
		text = b.formatGlances()
	case "stats":
		text = b.formatStats()
	default:
		text = "Unknown command. Use /help."
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
	var text string
	var markup *tgbotapi.InlineKeyboardMarkup

	switch {
	case data == "status:all":
		text = b.formatStatusAll()
		markup = b.statusKeyboard("")
	case strings.HasPrefix(data, "status:group:"):
		group := strings.TrimPrefix(data, "status:group:")
		text = b.formatStatusGroup(group)
		markup = b.statusKeyboard(group)
	case strings.HasPrefix(data, "status:host:"):
		host := strings.TrimPrefix(data, "status:host:")
		text = b.formatHost(host)
	default:
		text = "Unknown action"
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
	b.sendToAll(text)
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
		"/start - brief help",
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
		sb.WriteString(fmt.Sprintf("• <b>%s</b> [%s] — %s\n", h.Name, group, h.Status))
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
		sb.WriteString(fmt.Sprintf("• <b>%s</b>: %s\n", h.Name, h.Status))
	}
	return sb.String()
}

func (b *Bot) formatHost(name string) string {
	h, ok := b.cache.GetHost(name)
	if !ok {
		return fmt.Sprintf("Host %q not found.", name)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b> — %s\n", h.Name, h.Status))
	if h.Description != "" {
		sb.WriteString(escapeHTML(h.Description) + "\n")
	}
	if h.Group != "" {
		sb.WriteString(fmt.Sprintf("Group: %s\n", escapeHTML(h.Group)))
	}
	for _, ch := range h.Checks {
		sb.WriteString(fmt.Sprintf("\n<b>%s</b>: %s\n", ch.CheckType, ch.Status))
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
		sb.WriteString(fmt.Sprintf("• %s: %s", s.HostName, s.Status))
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

func (b *Bot) statusKeyboard(activeGroup string) *tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("All", "status:all")},
	}
	for _, g := range b.cache.Groups() {
		label := "Group: " + g
		if g == activeGroup {
			label = "• " + label
		}
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(label, "status:group:"+g),
		})
	}
	for _, h := range b.cache.ListHosts() {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Device: "+h.Name, "status:host:"+h.Name),
		})
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
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
