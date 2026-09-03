package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"goarmmon/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const sessionTTL = 15 * time.Minute

const (
	cbHostAdd      = "host:add"
	cbHostManage   = "host:manage"
	cbHostEdit     = "hedit:"
	cbHostField    = "hfield:"
	cbHostCheck    = "hcheck:"
	cbHostCheckAdd = "hckadd:"
	cbHostCheckRm  = "hckrm:"
	cbHostAlert    = "halert:"
	cbHostAlertDis = "halertdis:"
	cbHostAlertFld = "hafld:"
	cbHostToggle   = "htoggle:"
	cbHostDel      = "host:del:"
	cbHostDelYes   = "host:del_yes:"
	cbHostDelNo    = "host:del_no:"
	cbHostEditFrom = "heditfrom:"
)

type editSession struct {
	mode      string
	hostIdx   int
	field     string
	expiresAt time.Time
}

type hostEditor struct {
	mu       sync.Mutex
	sessions map[int64]*editSession
}

func newHostEditor() *hostEditor {
	return &hostEditor{sessions: make(map[int64]*editSession)}
}

func (e *hostEditor) get(userID int64) (*editSession, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[userID]
	if !ok {
		return nil, false
	}
	if time.Now().After(s.expiresAt) {
		delete(e.sessions, userID)
		return nil, false
	}
	return s, true
}

func (e *hostEditor) set(userID int64, s *editSession) {
	s.expiresAt = time.Now().Add(sessionTTL)
	e.mu.Lock()
	e.sessions[userID] = s
	e.mu.Unlock()
}

func (e *hostEditor) clear(userID int64) {
	e.mu.Lock()
	delete(e.sessions, userID)
	e.mu.Unlock()
}

func (b *Bot) SetConfigStore(store ConfigStore, ctx context.Context) {
	b.store = store
	b.runCtx = ctx
	if b.hostEdit == nil {
		b.hostEdit = newHostEditor()
	}
}

func (b *Bot) handleHostCallback(data string, userID int64, chatID int64, messageID int) (text string, markup *tgbotapi.InlineKeyboardMarkup, handled bool) {
	if b.store == nil {
		return "", nil, false
	}

	switch {
	case data == cbHostAdd:
		b.hostEdit.set(userID, &editSession{mode: "add_name"})
		return "<b>Add host</b>\nSend the host name:", nil, true
	case data == cbHostManage:
		return b.formatHostManageList(), b.hostManageListKeyboard(), true
	case strings.HasPrefix(data, cbHostEditFrom):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostEditFrom))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
	case strings.HasPrefix(data, cbHostEdit):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostEdit))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
	case strings.HasPrefix(data, cbHostDelNo):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostDelNo))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
	case strings.HasPrefix(data, cbHostDel):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostDel))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		hosts := b.store.HostConfigs()
		if idx < 0 || idx >= len(hosts) {
			return "Host not found.", b.hostManageListKeyboard(), true
		}
		return fmt.Sprintf("<b>Delete host?</b>\n\nHost: <b>%s</b>\nThis cannot be undone.", escapeHTML(hosts[idx].Name)),
			b.hostDeleteConfirmKeyboard(idx), true
	case strings.HasPrefix(data, cbHostDelYes):
		idx, ok := parseIdx(strings.TrimPrefix(data, cbHostDelYes))
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		err := b.deleteHost(idx)
		if err != nil {
			return fmt.Sprintf("Delete failed: %s", escapeHTML(err.Error())), b.hostManageListKeyboard(), true
		}
		b.hostEdit.clear(userID)
		return "<b>Host deleted.</b>\n\n" + b.formatHostManageList(), b.hostManageListKeyboard(), true
	case strings.HasPrefix(data, cbHostCheckAdd):
		rest := strings.TrimPrefix(data, cbHostCheckAdd)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		checkType := parts[1]
		err := b.addCheck(idx, checkType)
		if err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.hostCheckKeyboard(idx, checkType), true
		}
		return b.formatHostCheckSection(idx, checkType), b.hostCheckKeyboard(idx, checkType), true
	case strings.HasPrefix(data, cbHostCheckRm):
		rest := strings.TrimPrefix(data, cbHostCheckRm)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		checkType := parts[1]
		err := b.removeCheck(idx, checkType)
		if err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.hostManageCardKeyboard(idx), true
		}
		return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
	case strings.HasPrefix(data, cbHostCheck):
		rest := strings.TrimPrefix(data, cbHostCheck)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		checkType := parts[1]
		if checkType == "menu" {
			return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
		}
		return b.formatHostCheckSection(idx, checkType), b.hostCheckKeyboard(idx, checkType), true
	case strings.HasPrefix(data, cbHostAlertDis):
		rest := strings.TrimPrefix(data, cbHostAlertDis)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		alertKey := parts[1]
		err := b.disableAlert(idx, alertKey)
		if err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.hostAlertKeyboard(idx, alertKey), true
		}
		return b.formatHostAlertSection(idx, alertKey), b.hostAlertKeyboard(idx, alertKey), true
	case strings.HasPrefix(data, cbHostAlertFld):
		rest := strings.TrimPrefix(data, cbHostAlertFld)
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		alertKey, fieldKey := parts[1], parts[2]
		fieldPath := fmt.Sprintf("alerts.%s.%s", alertKey, fieldKey)
		b.hostEdit.set(userID, &editSession{mode: "field", hostIdx: idx, field: fieldPath})
		prompt := b.fieldPrompt(idx, fieldPath)
		return prompt, nil, true
	case strings.HasPrefix(data, cbHostAlert):
		rest := strings.TrimPrefix(data, cbHostAlert)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		alertKey := parts[1]
		if alertKey == "menu" {
			return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
		}
		if alertKey == "for" {
			b.hostEdit.set(userID, &editSession{mode: "field", hostIdx: idx, field: "alerts.for"})
			return b.fieldPrompt(idx, "alerts.for"), nil, true
		}
		return b.formatHostAlertSection(idx, alertKey), b.hostAlertKeyboard(idx, alertKey), true
	case strings.HasPrefix(data, cbHostToggle):
		rest := strings.TrimPrefix(data, cbHostToggle)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		field := parts[1]
		err := b.toggleField(idx, field)
		if err != nil {
			return fmt.Sprintf("Failed: %s", escapeHTML(err.Error())), b.hostManageCardKeyboard(idx), true
		}
		return b.formatHostManageCard(idx), b.hostManageCardKeyboard(idx), true
	case strings.HasPrefix(data, cbHostField):
		rest := strings.TrimPrefix(data, cbHostField)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "Invalid action.", mainMenuKeyboard(), true
		}
		idx, ok := parseIdx(parts[0])
		if !ok {
			return "Invalid host.", mainMenuKeyboard(), true
		}
		fieldPath := parts[1]
		b.hostEdit.set(userID, &editSession{mode: "field", hostIdx: idx, field: fieldPath})
		return b.fieldPrompt(idx, fieldPath), nil, true
	}
	return "", nil, false
}

func (b *Bot) handleHostTextInput(msg *tgbotapi.Message) bool {
	if b.store == nil {
		return false
	}
	userID := msg.From.ID
	s, ok := b.hostEdit.get(userID)
	if !ok {
		return false
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		b.replyPlain(msg.Chat.ID, "Empty input. Try again or /cancel.")
		return true
	}

	switch s.mode {
	case "add_name":
		if err := validateHostName(text); err != nil {
			b.replyPlain(msg.Chat.ID, err.Error()+"\nTry again or /cancel.")
			return true
		}
		for _, h := range b.store.HostConfigs() {
			if h.Name == text {
				b.replyPlain(msg.Chat.ID, fmt.Sprintf("Host %q already exists.\nTry again or /cancel.", text))
				return true
			}
		}
		idx := -1
		err := b.store.MutateConfig(func(cfg *config.Config) error {
			cfg.Hosts = append(cfg.Hosts, config.DefaultHost(text))
			idx = len(cfg.Hosts) - 1
			return nil
		})
		b.hostEdit.clear(userID)
		if err != nil {
			b.replyPlain(msg.Chat.ID, fmt.Sprintf("Failed to add host: %s", err.Error()))
			return true
		}
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("<b>Host added</b>\n\n%s", b.formatHostManageCard(idx)))
		reply.ParseMode = tgbotapi.ModeHTML
		reply.ReplyMarkup = b.hostManageCardKeyboard(idx)
		if _, err := b.send(reply); err != nil {
			slogWarnSend(err)
		}
		return true

	case "field":
		err := b.applyFieldEdit(s.hostIdx, s.field, text)
		if err != nil {
			b.replyPlain(msg.Chat.ID, fmt.Sprintf("Invalid value: %s\nTry again or /cancel.", err.Error()))
			return true
		}
		b.hostEdit.clear(userID)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("<b>Saved</b>\n\n%s", b.formatHostManageCard(s.hostIdx)))
		reply.ParseMode = tgbotapi.ModeHTML
		reply.ReplyMarkup = b.hostManageCardKeyboard(s.hostIdx)
		if _, err := b.send(reply); err != nil {
			slogWarnSend(err)
		}
		return true
	}
	return false
}

func (b *Bot) cancelHostEdit(userID int64) string {
	b.hostEdit.clear(userID)
	return "Edit cancelled."
}

func parseIdx(s string) (int, bool) {
	i, err := strconv.Atoi(s)
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

func validateHostName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("name must not contain whitespace")
	}
	return nil
}

func isClearValue(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "-" || s == "clear" || s == "none" || s == "off"
}

func parseOptionalFloat(s string) (*float64, error) {
	if isClearValue(s) {
		return nil, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil, fmt.Errorf("expected number or clear")
	}
	return &v, nil
}

func parseOptionalString(s string) (*string, error) {
	if isClearValue(s) {
		return nil, nil
	}
	v := strings.TrimSpace(s)
	return &v, nil
}

func parseStringList(s string) ([]string, error) {
	if isClearValue(s) {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func parseBoolInput(s string) (bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected true/false, on/off, yes/no")
	}
}

func (b *Bot) deleteHost(idx int) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		if idx < 0 || idx >= len(cfg.Hosts) {
			return fmt.Errorf("host not found")
		}
		cfg.Hosts = append(cfg.Hosts[:idx], cfg.Hosts[idx+1:]...)
		return nil
	})
}

func (b *Bot) addCheck(idx int, checkType string) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		h, ok := cfg.HostByIndex(idx)
		if !ok {
			return fmt.Errorf("host not found")
		}
		if config.HasCheck(*h, checkType) {
			return fmt.Errorf("check %q already exists", checkType)
		}
		switch checkType {
		case "ping":
			return config.SetPingCheck(h, config.PingCheckConfig{
				Type:          "ping",
				Timeout:       "3s",
				Interval:      "5s",
				FailThreshold: 3,
			})
		case "http":
			return config.SetHTTPCheck(h, config.DefaultHTTPCheck())
		case "glances":
			return config.SetGlancesCheck(h, config.DefaultGlancesCheck())
		default:
			return fmt.Errorf("unknown check type %q", checkType)
		}
	})
}

func (b *Bot) removeCheck(idx int, checkType string) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		h, ok := cfg.HostByIndex(idx)
		if !ok {
			return fmt.Errorf("host not found")
		}
		if !config.RemoveCheck(h, checkType) {
			return fmt.Errorf("check %q not found", checkType)
		}
		if len(h.Checks) == 0 {
			return fmt.Errorf("at least one check is required")
		}
		return nil
	})
}

func (b *Bot) toggleField(idx int, field string) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		h, ok := cfg.HostByIndex(idx)
		if !ok {
			return fmt.Errorf("host not found")
		}
		switch field {
		case "skip_on_ping_failure":
			h.SkipOnPingFailure = !h.SkipOnPingFailure
		case "checks.http.follow_redirects":
			cfgCheck, _, ok := config.GetHTTPCheck(h)
			if !ok {
				return fmt.Errorf("http check not configured")
			}
			cfgCheck.FollowRedirects = !cfgCheck.FollowRedirects
			return config.SetHTTPCheck(h, *cfgCheck)
		default:
			return fmt.Errorf("unknown toggle field")
		}
		return nil
	})
}

func (b *Bot) disableAlert(idx int, alertKey string) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		h, ok := cfg.HostByIndex(idx)
		if !ok {
			return fmt.Errorf("host not found")
		}
		switch alertKey {
		case "cpu":
			h.Alerts.CPU = nil
		case "ram":
			h.Alerts.RAM = nil
		case "swap":
			h.Alerts.Swap = nil
		case "disk":
			h.Alerts.Disk = nil
		case "rtt":
			h.Alerts.RTT = nil
		case "http_response":
			h.Alerts.HTTPResponse = nil
		default:
			return fmt.Errorf("unknown alert")
		}
		return nil
	})
}

func (b *Bot) applyFieldEdit(idx int, fieldPath, value string) error {
	return b.store.MutateConfig(func(cfg *config.Config) error {
		h, ok := cfg.HostByIndex(idx)
		if !ok {
			return fmt.Errorf("host not found")
		}
		return applyFieldValue(h, cfg, idx, fieldPath, value)
	})
}

func applyFieldValue(h *config.HostConfig, cfg *config.Config, idx int, fieldPath, value string) error {
	switch fieldPath {
	case "name":
		if err := validateHostName(value); err != nil {
			return err
		}
		for i, other := range cfg.Hosts {
			if i != idx && other.Name == value {
				return fmt.Errorf("host %q already exists", value)
			}
		}
		h.Name = value
	case "description":
		h.Description = value
	case "group":
		h.Group = value
	case "alerts.for":
		v, err := parseOptionalString(value)
		if err != nil {
			return err
		}
		h.Alerts.For = v
	default:
		if strings.HasPrefix(fieldPath, "messages.") {
			return applyMessageField(h, fieldPath, value)
		}
		if strings.HasPrefix(fieldPath, "checks.ping.") {
			return applyPingField(h, fieldPath, value)
		}
		if strings.HasPrefix(fieldPath, "checks.http.") {
			return applyHTTPField(h, fieldPath, value)
		}
		if strings.HasPrefix(fieldPath, "checks.glances.") {
			return applyGlancesField(h, fieldPath, value)
		}
		if strings.HasPrefix(fieldPath, "alerts.") {
			return applyAlertField(h, fieldPath, value)
		}
		return fmt.Errorf("unknown field %q", fieldPath)
	}
	return nil
}

func applyMessageField(h *config.HostConfig, fieldPath, value string) error {
	text := ""
	if !isClearValue(value) {
		text = strings.TrimSpace(value)
	}
	switch fieldPath {
	case "messages.offline":
		h.Messages.Offline = text
	case "messages.online":
		h.Messages.Online = text
	case "messages.warning":
		h.Messages.Warning = text
	case "messages.critical":
		h.Messages.Critical = text
	case "messages.recovery":
		h.Messages.Recovery = text
	default:
		return fmt.Errorf("unknown message field")
	}
	return nil
}

func applyPingField(h *config.HostConfig, fieldPath, value string) error {
	cfg, _, ok := config.GetPingCheck(h)
	if !ok {
		return fmt.Errorf("ping check not configured")
	}
	switch fieldPath {
	case "checks.ping.address":
		cfg.Address = strings.TrimSpace(value)
	case "checks.ping.timeout":
		if _, err := config.ParseDuration(value, "timeout"); err != nil {
			return err
		}
		cfg.Timeout = strings.TrimSpace(value)
	case "checks.ping.interval":
		if _, err := config.ParseDuration(value, "interval"); err != nil {
			return err
		}
		cfg.Interval = strings.TrimSpace(value)
	case "checks.ping.fail_threshold":
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || v <= 0 {
			return fmt.Errorf("expected positive integer")
		}
		cfg.FailThreshold = v
	default:
		return fmt.Errorf("unknown ping field")
	}
	return config.SetPingCheck(h, *cfg)
}

func applyHTTPField(h *config.HostConfig, fieldPath, value string) error {
	cfg, _, ok := config.GetHTTPCheck(h)
	if !ok {
		return fmt.Errorf("http check not configured")
	}
	switch fieldPath {
	case "checks.http.url":
		cfg.URL = strings.TrimSpace(value)
	case "checks.http.method":
		m := strings.ToUpper(strings.TrimSpace(value))
		if m != "GET" && m != "HEAD" {
			return fmt.Errorf("method must be GET or HEAD")
		}
		cfg.Method = m
	case "checks.http.expected_code":
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("expected integer status code")
		}
		cfg.ExpectedCode = v
	case "checks.http.timeout":
		if _, err := config.ParseDuration(value, "timeout"); err != nil {
			return err
		}
		cfg.Timeout = strings.TrimSpace(value)
	case "checks.http.interval":
		if _, err := config.ParseDuration(value, "interval"); err != nil {
			return err
		}
		cfg.Interval = strings.TrimSpace(value)
	default:
		return fmt.Errorf("unknown http field")
	}
	return config.SetHTTPCheck(h, *cfg)
}

func applyGlancesField(h *config.HostConfig, fieldPath, value string) error {
	cfg, _, ok := config.GetGlancesCheck(h)
	if !ok {
		return fmt.Errorf("glances check not configured")
	}
	switch fieldPath {
	case "checks.glances.url":
		cfg.URL = strings.TrimSpace(value)
	case "checks.glances.api_version":
		cfg.APIVersion = strings.TrimSpace(value)
	case "checks.glances.username":
		cfg.Username = strings.TrimSpace(value)
	case "checks.glances.password":
		cfg.Password = strings.TrimSpace(value)
	case "checks.glances.token":
		cfg.Token = strings.TrimSpace(value)
	case "checks.glances.timeout":
		if _, err := config.ParseDuration(value, "timeout"); err != nil {
			return err
		}
		cfg.Timeout = strings.TrimSpace(value)
	case "checks.glances.interval":
		if _, err := config.ParseDuration(value, "interval"); err != nil {
			return err
		}
		cfg.Interval = strings.TrimSpace(value)
	default:
		return fmt.Errorf("unknown glances field")
	}
	return config.SetGlancesCheck(h, *cfg)
}

func applyAlertField(h *config.HostConfig, fieldPath, value string) error {
	parts := strings.Split(fieldPath, ".")
	if len(parts) < 3 {
		return fmt.Errorf("invalid alert field")
	}
	alertKey, subField := parts[1], parts[2]

	if alertKey == "disk" && len(parts) == 3 && (subField == "ignore_devices" || subField == "ignore_mounts") {
		list, err := parseStringList(value)
		if err != nil {
			return err
		}
		disk := h.Alerts.Disk
		if disk == nil {
			disk = &config.DiskThreshold{}
		}
		if subField == "ignore_devices" {
			disk.IgnoreDevices = list
		} else {
			disk.IgnoreMounts = list
		}
		h.Alerts.Disk = disk
		return nil
	}

	var thr *config.Threshold
	switch alertKey {
	case "cpu":
		if h.Alerts.CPU == nil {
			h.Alerts.CPU = &config.Threshold{}
		}
		thr = h.Alerts.CPU
	case "ram":
		if h.Alerts.RAM == nil {
			h.Alerts.RAM = &config.Threshold{}
		}
		thr = h.Alerts.RAM
	case "swap":
		if h.Alerts.Swap == nil {
			h.Alerts.Swap = &config.Threshold{}
		}
		thr = h.Alerts.Swap
	case "disk":
		if h.Alerts.Disk == nil {
			h.Alerts.Disk = &config.DiskThreshold{}
		}
		thr = &h.Alerts.Disk.Threshold
	case "rtt":
		if h.Alerts.RTT == nil {
			h.Alerts.RTT = &config.Threshold{}
		}
		thr = h.Alerts.RTT
	case "http_response":
		if h.Alerts.HTTPResponse == nil {
			h.Alerts.HTTPResponse = &config.Threshold{}
		}
		thr = h.Alerts.HTTPResponse
	default:
		return fmt.Errorf("unknown alert %q", alertKey)
	}

	switch subField {
	case "warning", "critical", "recovery":
		v, err := parseOptionalFloat(value)
		if err != nil {
			return err
		}
		switch subField {
		case "warning":
			thr.Warning = v
		case "critical":
			thr.Critical = v
		case "recovery":
			thr.Recovery = v
		}
	case "for":
		v, err := parseOptionalString(value)
		if err != nil {
			return err
		}
		if v != nil {
			if _, err := config.ParseDuration(*v, "for"); err != nil {
				return err
			}
		}
		thr.For = v
	default:
		return fmt.Errorf("unknown alert sub-field %q", subField)
	}
	return nil
}

func (b *Bot) fieldPrompt(idx int, fieldPath string) string {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "Host not found."
	}
	h := hosts[idx]
	current := formatFieldValue(h, fieldPath)
	desc := fieldDescription(fieldPath)
	return fmt.Sprintf("<b>Edit field</b>\nHost: <b>%s</b>\nField: %s\nCurrent: %s\n\n%s\n\nSend new value or /cancel.\nUse <code>-</code> or <code>clear</code> to reset optional fields.",
		escapeHTML(h.Name), escapeHTML(fieldPath), escapeHTML(current), desc)
}

func fieldDescription(fieldPath string) string {
	switch fieldPath {
	case "name":
		return "Unique host identifier (no spaces)."
	case "description":
		return "Human-readable description."
	case "group":
		return "Group label for filtering in status view."
	case "messages.offline":
		return "Custom text for OFFLINE notifications. Empty uses the default technical message."
	case "messages.online":
		return "Custom text for ONLINE notifications. Empty uses the default technical message."
	case "messages.warning":
		return "Custom text for WARNING notifications. Empty uses the default technical message."
	case "messages.critical":
		return "Custom text for CRITICAL notifications. Empty uses the default technical message."
	case "messages.recovery":
		return "Custom text for RECOVERY notifications. Empty uses the default technical message."
	case "alerts.for":
		return "Default alert forbearance duration (e.g. <code>2m</code>, <code>30s</code>)."
	case "checks.ping.address":
		return "Ping target IP or hostname."
	case "checks.ping.timeout":
		return "Ping timeout (e.g. <code>3s</code>)."
	case "checks.ping.interval":
		return "Ping interval (e.g. <code>5s</code>)."
	case "checks.ping.fail_threshold":
		return "Consecutive failures before offline alert."
	case "checks.http.url":
		return "HTTP check URL."
	case "checks.http.method":
		return "HTTP method: GET or HEAD."
	case "checks.http.expected_code":
		return "Expected HTTP status code."
	case "checks.http.timeout", "checks.http.interval":
		return "Duration (e.g. <code>5s</code>, <code>30s</code>)."
	case "checks.glances.url":
		return "Glances API base URL."
	case "checks.glances.api_version":
		return "API version: <code>auto</code>, <code>v3</code>, or <code>v4</code>."
	case "checks.glances.username", "checks.glances.password", "checks.glances.token":
		return "Authentication credentials (optional)."
	case "checks.glances.timeout", "checks.glances.interval":
		return "Duration (e.g. <code>10s</code>, <code>15s</code>)."
	case "alerts.disk.ignore_devices", "alerts.disk.ignore_mounts":
		return "Comma-separated patterns (supports <code>*</code> wildcards)."
	default:
		if strings.HasPrefix(fieldPath, "alerts.") && strings.HasSuffix(fieldPath, ".for") {
			return "Per-metric forbearance duration (e.g. <code>2m</code>)."
		}
		if strings.HasPrefix(fieldPath, "alerts.") {
			return "Threshold value (number). Use clear to remove."
		}
		return ""
	}
}

func formatFieldValue(h config.HostConfig, fieldPath string) string {
	switch fieldPath {
	case "name":
		return h.Name
	case "description":
		return h.Description
	case "group":
		return h.Group
	case "alerts.for":
		return ptrStr(h.Alerts.For)
	case "messages.offline":
		return emptyDash(h.Messages.Offline)
	case "messages.online":
		return emptyDash(h.Messages.Online)
	case "messages.warning":
		return emptyDash(h.Messages.Warning)
	case "messages.critical":
		return emptyDash(h.Messages.Critical)
	case "messages.recovery":
		return emptyDash(h.Messages.Recovery)
	default:
		if strings.HasPrefix(fieldPath, "checks.ping.") {
			cfg, _, ok := config.GetPingCheck(&h)
			if !ok {
				return "-"
			}
			switch fieldPath {
			case "checks.ping.address":
				return cfg.Address
			case "checks.ping.timeout":
				return cfg.Timeout
			case "checks.ping.interval":
				return cfg.Interval
			case "checks.ping.fail_threshold":
				return strconv.Itoa(cfg.FailThreshold)
			}
		}
		if strings.HasPrefix(fieldPath, "checks.http.") {
			cfg, _, ok := config.GetHTTPCheck(&h)
			if !ok {
				return "-"
			}
			switch fieldPath {
			case "checks.http.url":
				return cfg.URL
			case "checks.http.method":
				return cfg.Method
			case "checks.http.expected_code":
				return strconv.Itoa(cfg.ExpectedCode)
			case "checks.http.timeout":
				return cfg.Timeout
			case "checks.http.interval":
				return cfg.Interval
			}
		}
		if strings.HasPrefix(fieldPath, "checks.glances.") {
			cfg, _, ok := config.GetGlancesCheck(&h)
			if !ok {
				return "-"
			}
			switch fieldPath {
			case "checks.glances.url":
				return cfg.URL
			case "checks.glances.api_version":
				return cfg.APIVersion
			case "checks.glances.username":
				return cfg.Username
			case "checks.glances.password":
				if cfg.Password != "" {
					return "***"
				}
				return ""
			case "checks.glances.token":
				if cfg.Token != "" {
					return "***"
				}
				return ""
			case "checks.glances.timeout":
				return cfg.Timeout
			case "checks.glances.interval":
				return cfg.Interval
			}
		}
		if strings.HasPrefix(fieldPath, "alerts.") {
			return formatAlertFieldValue(h, fieldPath)
		}
	}
	return "-"
}

func formatAlertFieldValue(h config.HostConfig, fieldPath string) string {
	parts := strings.Split(fieldPath, ".")
	if len(parts) < 3 {
		return "-"
	}
	alertKey, subField := parts[1], parts[2]

	if alertKey == "disk" && subField == "ignore_devices" {
		if h.Alerts.Disk == nil {
			return "-"
		}
		return strings.Join(h.Alerts.Disk.IgnoreDevices, ", ")
	}
	if alertKey == "disk" && subField == "ignore_mounts" {
		if h.Alerts.Disk == nil {
			return "-"
		}
		return strings.Join(h.Alerts.Disk.IgnoreMounts, ", ")
	}

	var thr *config.Threshold
	switch alertKey {
	case "cpu":
		thr = h.Alerts.CPU
	case "ram":
		thr = h.Alerts.RAM
	case "swap":
		thr = h.Alerts.Swap
	case "disk":
		if h.Alerts.Disk != nil {
			thr = &h.Alerts.Disk.Threshold
		}
	case "rtt":
		thr = h.Alerts.RTT
	case "http_response":
		thr = h.Alerts.HTTPResponse
	}
	if thr == nil {
		return "-"
	}
	switch subField {
	case "warning":
		return ptrFloat(thr.Warning)
	case "critical":
		return ptrFloat(thr.Critical)
	case "recovery":
		return ptrFloat(thr.Recovery)
	case "for":
		return ptrStr(thr.For)
	}
	return "-"
}

func ptrStr(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func ptrFloat(f *float64) string {
	if f == nil {
		return "-"
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

func (b *Bot) formatHostManageList() string {
	hosts := b.store.HostConfigs()
	if len(hosts) == 0 {
		return "<b>Manage hosts</b>\nNo hosts configured."
	}
	var sb strings.Builder
	sb.WriteString("<b>Manage hosts</b>\nSelect a host to edit:\n")
	for i, h := range hosts {
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>", i+1, escapeHTML(h.Name)))
		if h.Group != "" {
			sb.WriteString(fmt.Sprintf(" [%s]", escapeHTML(h.Group)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (b *Bot) formatHostManageCard(idx int) string {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "Host not found."
	}
	h := hosts[idx]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Manage: %s</b>\n", escapeHTML(h.Name)))
	if h.Description != "" {
		sb.WriteString(escapeHTML(h.Description) + "\n")
	}
	if h.Group != "" {
		sb.WriteString(fmt.Sprintf("Group: %s\n", escapeHTML(h.Group)))
	}
	sb.WriteString(fmt.Sprintf("skip_on_ping_failure: %v\n", h.SkipOnPingFailure))
	if kinds := h.Messages.ConfiguredKinds(); len(kinds) > 0 {
		sb.WriteString("Messages: " + strings.Join(kinds, ", ") + "\n")
	} else {
		sb.WriteString("Messages: default\n")
	}
	sb.WriteString("\n<b>Checks:</b> " + strings.Join(config.ListCheckTypes(h), ", ") + "\n")
	sb.WriteString("\nUse buttons below to edit sections.")
	return sb.String()
}

func (b *Bot) formatHostCheckSection(idx int, checkType string) string {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "Host not found."
	}
	h := hosts[idx]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Check: %s</b> — %s\n\n", checkType, escapeHTML(h.Name)))
	switch checkType {
	case "ping":
		cfg, _, ok := config.GetPingCheck(&h)
		if !ok {
			sb.WriteString("Not configured.")
			return sb.String()
		}
		sb.WriteString(fmt.Sprintf("address: %s\n", escapeHTML(cfg.Address)))
		sb.WriteString(fmt.Sprintf("timeout: %s\n", cfg.Timeout))
		sb.WriteString(fmt.Sprintf("interval: %s\n", cfg.Interval))
		sb.WriteString(fmt.Sprintf("fail_threshold: %d\n", cfg.FailThreshold))
	case "http":
		cfg, _, ok := config.GetHTTPCheck(&h)
		if !ok {
			sb.WriteString("Not configured.")
			return sb.String()
		}
		sb.WriteString(fmt.Sprintf("url: %s\n", escapeHTML(cfg.URL)))
		sb.WriteString(fmt.Sprintf("method: %s\n", cfg.Method))
		sb.WriteString(fmt.Sprintf("expected_code: %d\n", cfg.ExpectedCode))
		sb.WriteString(fmt.Sprintf("follow_redirects: %v\n", cfg.FollowRedirects))
		sb.WriteString(fmt.Sprintf("timeout: %s\n", cfg.Timeout))
		sb.WriteString(fmt.Sprintf("interval: %s\n", cfg.Interval))
	case "glances":
		cfg, _, ok := config.GetGlancesCheck(&h)
		if !ok {
			sb.WriteString("Not configured.")
			return sb.String()
		}
		sb.WriteString(fmt.Sprintf("url: %s\n", escapeHTML(cfg.URL)))
		sb.WriteString(fmt.Sprintf("api_version: %s\n", cfg.APIVersion))
		sb.WriteString(fmt.Sprintf("timeout: %s\n", cfg.Timeout))
		sb.WriteString(fmt.Sprintf("interval: %s\n", cfg.Interval))
		if cfg.Username != "" {
			sb.WriteString("username: set\n")
		}
		if cfg.Password != "" {
			sb.WriteString("password: set\n")
		}
		if cfg.Token != "" {
			sb.WriteString("token: set\n")
		}
	}
	return sb.String()
}

func (b *Bot) formatHostAlertSection(idx int, alertKey string) string {
	hosts := b.store.HostConfigs()
	if idx < 0 || idx >= len(hosts) {
		return "Host not found."
	}
	h := hosts[idx]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Alert: %s</b> — %s\n\n", alertKey, escapeHTML(h.Name)))

	var thr *config.Threshold
	var disk *config.DiskThreshold
	switch alertKey {
	case "cpu":
		thr = h.Alerts.CPU
	case "ram":
		thr = h.Alerts.RAM
	case "swap":
		thr = h.Alerts.Swap
	case "disk":
		disk = h.Alerts.Disk
		if disk != nil {
			thr = &disk.Threshold
		}
	case "rtt":
		thr = h.Alerts.RTT
	case "http_response":
		thr = h.Alerts.HTTPResponse
	}
	if thr == nil {
		sb.WriteString("Not configured.")
	} else {
		sb.WriteString(fmt.Sprintf("warning: %s\n", ptrFloat(thr.Warning)))
		sb.WriteString(fmt.Sprintf("critical: %s\n", ptrFloat(thr.Critical)))
		sb.WriteString(fmt.Sprintf("recovery: %s\n", ptrFloat(thr.Recovery)))
		sb.WriteString(fmt.Sprintf("for: %s\n", ptrStr(thr.For)))
	}
	if alertKey == "disk" && disk != nil {
		sb.WriteString(fmt.Sprintf("ignore_devices: %s\n", strings.Join(disk.IgnoreDevices, ", ")))
		sb.WriteString(fmt.Sprintf("ignore_mounts: %s\n", strings.Join(disk.IgnoreMounts, ", ")))
	}
	return sb.String()
}

func (b *Bot) replyPlain(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.send(msg); err != nil {
		slogWarnSend(err)
	}
}

func slogWarnSend(err error) {
	slog.Warn("telegram send failed", "error", err)
}

func hostIndexByName(store ConfigStore, name string) (int, bool) {
	if store == nil {
		return -1, false
	}
	hosts := store.HostConfigs()
	for i, h := range hosts {
		if h.Name == name {
			return i, true
		}
	}
	return -1, false
}
