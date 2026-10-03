package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

const TelegramCallbackPath = "/api/v1/plugin/binding/telegram/callback"

var ErrTelegramBinding = errors.New("Telegram 绑定已失效，请重新授权")
var telegramTokenPattern = regexp.MustCompile(`^([1-9][0-9]{0,18}):[A-Za-z0-9_-]{20,100}$`)
var telegramUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)

type TelegramNotice string

func (notice TelegramNotice) Error() string { return string(notice) }

type TelegramBindingStatus struct {
	Ticket      string    `json:"ticket,omitempty"`
	URL         string    `json:"url,omitempty"`
	Phase       string    `json:"phase"`
	DisplayName string    `json:"displayName,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type telegramBinding struct {
	Actor       NotificationBindingActor
	Version     uint64
	Fingerprint string
	CreatedAt   time.Time
	Settings    TelegramUserSettings
	Status      TelegramBindingStatus
}

type telegramBindingConfig struct {
	Token       string
	Proxy       string
	TimeoutMS   int
	BotID       string
	Callback    string
	Secret      string
	Fingerprint string
}

func (s *ConfigService) telegramConfiguration() (telegramBindingConfig, error) {
	var result telegramBindingConfig
	manager, current := config.CurrentManager(), config.CurrentConfig()
	if s == nil || s.vault == nil || manager == nil || current == nil {
		return result, TelegramNotice("请联系超级管理员配置 Telegram 机器人")
	}
	instance, found := manager.Snapshot().Plugins.Plugins[telegramPluginID]
	if !found || !instance.Enabled {
		return result, TelegramNotice("请联系超级管理员启用 Telegram 通知")
	}
	var settings struct {
		WebDomain string `json:"webDomain"`
		Proxy     string `json:"proxy"`
		Timeout   int    `json:"timeout"`
	}
	domain := strings.TrimSpace(current.WebDomain)
	parsed, err := url.Parse("https://" + domain)
	if json.Unmarshal(instance.Settings, &settings) != nil || err != nil || domain == "" || parsed.Host != domain ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!strings.EqualFold(settings.WebDomain, domain) {
		return result, TelegramNotice("机器人网页域名与后台域名不一致，请联系超级管理员")
	}
	proxyURL, err := credential.NormalizeTelegramProxyURL(settings.Proxy)
	if err != nil {
		return result, TelegramNotice(err.Error())
	}
	if settings.Timeout == 0 {
		settings.Timeout = 30
	}
	if settings.Timeout < 1 || settings.Timeout > 120 {
		return result, TelegramNotice("Telegram 请求超时时间无效")
	}
	token, err := s.openSecretForPlugin(telegramPluginID, "botToken")
	parts := telegramTokenPattern.FindStringSubmatch(token)
	if err != nil || len(parts) != 2 {
		return result, TelegramNotice("请联系超级管理员配置有效的机器人令牌")
	}
	fingerprint, err := s.vault.Fingerprint("telegram-binding\x00" + token + "\x00" + domain)
	if err != nil {
		return result, ErrPluginStoreUnavailable
	}
	secret, err := s.vault.Fingerprint("telegram-callback-secret\x00" + token + "\x00" + domain)
	if err != nil {
		return result, ErrPluginStoreUnavailable
	}
	// 回调地址只携带配置标识，不包含机器人令牌或请求认证密钥。
	return telegramBindingConfig{Token: token, Proxy: proxyURL, TimeoutMS: settings.Timeout * 1000, BotID: parts[1], Secret: secret, Fingerprint: fingerprint,
		Callback: "https://" + domain + TelegramCallbackPath + "?v=" + fingerprint[:16]}, nil
}

func requestTelegramBindingJSON(ctx context.Context, configuration telegramBindingConfig, method string, input any, target any) error {
	if method != "getMe" && method != "getWebhookInfo" && method != "setWebhook" {
		return ErrTelegramBinding
	}
	body, err := json.Marshal(input)
	if err != nil {
		return ErrTelegramBinding
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+configuration.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return ErrTelegramBinding
	}
	request.Header.Set("Content-Type", "application/json")
	client, err := credential.NewTelegramHTTPClient(configuration.TimeoutMS, configuration.Proxy)
	if err != nil {
		return TelegramNotice(err.Error())
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return TelegramNotice("无法连接 Telegram，请稍后重试")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err != nil || len(data) > 64<<10 || response.StatusCode != http.StatusOK || json.Unmarshal(data, &envelope) != nil || !envelope.OK {
		// 不把含令牌的请求地址、外部错误正文或回调密钥写入日志和响应。
		return TelegramNotice("Telegram 请求失败，请检查系统机器人配置")
	}
	if target != nil && json.Unmarshal(envelope.Result, target) != nil {
		return ErrTelegramBinding
	}
	return nil
}

type TelegramCallbackStatus struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

func (s *ConfigService) TelegramBindingAvailability() (bool, string) {
	if _, err := s.telegramConfiguration(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (s *ConfigService) TelegramCallbackState(ctx context.Context) (TelegramCallbackStatus, error) {
	configuration, err := s.telegramConfiguration()
	if err != nil {
		return TelegramCallbackStatus{}, err
	}
	var info struct {
		URL string `json:"url"`
	}
	if err := requestTelegramBindingJSON(ctx, configuration, "getWebhookInfo", struct{}{}, &info); err != nil {
		return TelegramCallbackStatus{}, err
	}
	if info.URL != configuration.Callback {
		return TelegramCallbackStatus{Message: "请联系超级管理员启用或更新 Telegram 授权回调"}, nil
	}
	return TelegramCallbackStatus{Ready: true}, nil
}

func (s *ConfigService) ConfigureTelegramCallback(ctx context.Context, actorUserID int) (TelegramCallbackStatus, error) {
	actor, err := s.activeNotificationUser(actorUserID)
	if err != nil || actor.EffectiveRole() != access.RoleSuperAdmin {
		return TelegramCallbackStatus{}, &access.Denial{Code: access.CodeAccessDenied}
	}
	s.telegramSetupMu.Lock()
	defer s.telegramSetupMu.Unlock()
	configuration, err := s.telegramConfiguration()
	if err != nil {
		return TelegramCallbackStatus{}, err
	}
	var info struct {
		URL string `json:"url"`
	}
	if err := requestTelegramBindingJSON(ctx, configuration, "getWebhookInfo", struct{}{}, &info); err != nil {
		return TelegramCallbackStatus{}, err
	}
	if info.URL != "" && info.URL != configuration.Callback {
		existing, parseErr := url.Parse(info.URL)
		wanted, _ := url.Parse(configuration.Callback)
		if parseErr != nil || existing.Scheme != "https" || existing.Host != wanted.Host || existing.User != nil ||
			existing.Path != TelegramCallbackPath || existing.Fragment != "" || len(existing.Query()) != 1 || existing.Query().Get("v") == "" {
			return TelegramCallbackStatus{}, TelegramNotice("机器人已有其他回调，未覆盖；请使用专用机器人或先确认原应用的接入方式")
		}
	}
	input := map[string]any{"url": configuration.Callback, "secret_token": configuration.Secret,
		"allowed_updates": []string{"message"}, "drop_pending_updates": false, "max_connections": 4}
	var accepted bool
	if err := requestTelegramBindingJSON(ctx, configuration, "setWebhook", input, &accepted); err != nil || !accepted {
		return TelegramCallbackStatus{}, TelegramNotice("Telegram 授权回调设置失败")
	}
	current, err := s.telegramConfiguration()
	if err != nil || current.Fingerprint != configuration.Fingerprint {
		return TelegramCallbackStatus{}, TelegramNotice("系统配置已变更，请重新设置授权回调")
	}
	return s.TelegramCallbackState(ctx)
}

func (s *ConfigService) reserveTelegramAttempt(userID int, action string, interval time.Duration) error {
	if _, err := s.activeNotificationUser(userID); err != nil {
		return err
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	now := s.currentTime()
	if s.telegramAttempts == nil {
		s.telegramAttempts = make(map[string]time.Time)
	}
	for key, expires := range s.telegramAttempts {
		if !expires.After(now) {
			delete(s.telegramAttempts, key)
		}
	}
	key := action + ":" + strconv.Itoa(userID)
	if _, found := s.telegramAttempts[key]; found || len(s.telegramAttempts) >= 4096 {
		return TelegramNotice("操作过于频繁，请稍后重试")
	}
	s.telegramAttempts[key] = now.Add(interval)
	return nil
}

func (s *ConfigService) BeginTelegramBinding(ctx context.Context, actor NotificationBindingActor) (TelegramBindingStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return TelegramBindingStatus{}, ErrTelegramBinding
	}
	if err := s.reserveTelegramAttempt(actor.UserID, "begin", 10*time.Second); err != nil {
		return TelegramBindingStatus{}, err
	}
	configuration, err := s.telegramConfiguration()
	if err != nil {
		return TelegramBindingStatus{}, err
	}
	callback, err := s.TelegramCallbackState(ctx)
	if err != nil {
		return TelegramBindingStatus{}, err
	}
	if !callback.Ready {
		return TelegramBindingStatus{}, TelegramNotice(callback.Message)
	}
	var me struct {
		ID       int64  `json:"id"`
		IsBot    bool   `json:"is_bot"`
		Username string `json:"username"`
	}
	if err := requestTelegramBindingJSON(ctx, configuration, "getMe", struct{}{}, &me); err != nil {
		return TelegramBindingStatus{}, err
	}
	if !me.IsBot || strconv.FormatInt(me.ID, 10) != configuration.BotID || !telegramUsernamePattern.MatchString(me.Username) {
		return TelegramBindingStatus{}, TelegramNotice("机器人身份无效，请联系超级管理员")
	}
	current, err := s.GetOwnTelegramSettings(actor.UserID)
	if err != nil {
		return TelegramBindingStatus{}, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return TelegramBindingStatus{}, ErrPluginStoreUnavailable
	}
	ticket := base64.RawURLEncoding.EncodeToString(nonce[:])
	now := s.currentTime()
	status := TelegramBindingStatus{Ticket: ticket, Phase: "waiting", ExpiresAt: now.Add(5 * time.Minute),
		URL: "https://t.me/" + me.Username + "?start=smail_" + ticket}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	if s.telegramBindings == nil {
		s.telegramBindings = make(map[string]telegramBinding)
	}
	for key, binding := range s.telegramBindings {
		if !binding.Status.ExpiresAt.After(now) || binding.Actor.UserID == actor.UserID {
			delete(s.telegramBindings, key)
		}
	}
	if len(s.telegramBindings) >= 2048 {
		return TelegramBindingStatus{}, TelegramNotice("绑定请求较多，请稍后重试")
	}
	s.telegramBindings[ticket] = telegramBinding{Actor: actor, Version: current.Version, Fingerprint: configuration.Fingerprint,
		CreatedAt: now, Settings: TelegramUserSettings{BotID: configuration.BotID, DisableLinkPreview: true}, Status: status}
	return status, nil
}

func (s *ConfigService) GetTelegramBinding(actor NotificationBindingActor, ticket string) (TelegramBindingStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return TelegramBindingStatus{}, ErrTelegramBinding
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	binding, found := s.telegramBindings[ticket]
	if !found || binding.Actor != actor || !binding.Status.ExpiresAt.After(s.currentTime()) {
		return TelegramBindingStatus{}, ErrTelegramBinding
	}
	return binding.Status, nil
}

func (s *ConfigService) CancelTelegramBinding(actor NotificationBindingActor, ticket string) error {
	if err := s.validateBindingActor(actor); err != nil {
		return ErrTelegramBinding
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	if binding, found := s.telegramBindings[ticket]; found && binding.Actor != actor {
		return ErrTelegramBinding
	}
	delete(s.telegramBindings, ticket)
	return nil
}

type TelegramBindingUpdate struct {
	Message *struct {
		Date          int64           `json:"date"`
		Text          string          `json:"text"`
		ForwardOrigin json.RawMessage `json:"forward_origin"`
		From          *struct {
			ID        int64  `json:"id"`
			IsBot     bool   `json:"is_bot"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

func (s *ConfigService) ClaimTelegramBinding(secret string, update TelegramBindingUpdate) error {
	configuration, err := s.telegramConfiguration()
	if err != nil || len(secret) != len(configuration.Secret) || secret == "" ||
		subtle.ConstantTimeCompare([]byte(secret), []byte(configuration.Secret)) != 1 {
		return ErrTelegramBinding
	}
	message := update.Message
	if message == nil || message.From == nil || message.From.IsBot || message.Chat.Type != "private" ||
		message.From.ID <= 0 || message.From.ID != message.Chat.ID || len(message.ForwardOrigin) != 0 {
		return nil
	}
	parts := strings.Fields(message.Text)
	if len(parts) != 2 || parts[0] != "/start" || !strings.HasPrefix(parts[1], "smail_") {
		return nil
	}
	ticket := strings.TrimPrefix(parts[1], "smail_")
	if len(ticket) != 43 {
		return nil
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	binding, found := s.telegramBindings[ticket]
	now := s.currentTime()
	if !found || binding.Status.Phase != "waiting" || !binding.Status.ExpiresAt.After(now) ||
		binding.Fingerprint != configuration.Fingerprint || message.Date < binding.CreatedAt.Unix() || message.Date > now.Add(time.Minute).Unix() {
		return nil
	}
	name := strings.TrimSpace(message.From.FirstName + " " + message.From.LastName)
	if message.From.Username != "" {
		name += " (@" + message.From.Username + ")"
	}
	if len([]rune(name)) > 160 {
		name = string([]rune(name)[:160])
	}
	if name == "" {
		name = "已授权的 Telegram 用户"
	}
	binding.Settings.ChatID = strconv.FormatInt(message.Chat.ID, 10)
	binding.Settings.DisplayName = name
	binding.Settings.Verified = true
	binding.Status.DisplayName, binding.Status.Phase = name, "claimed"
	s.telegramBindings[ticket] = binding
	return nil
}

func (s *ConfigService) ConfirmTelegramBinding(actor NotificationBindingActor, ticket string) (UserSettingsStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return UserSettingsStatus{}, ErrTelegramBinding
	}
	configuration, err := s.telegramConfiguration()
	if err != nil {
		return UserSettingsStatus{}, err
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	binding, found := s.telegramBindings[ticket]
	if !found || binding.Actor != actor || binding.Status.Phase != "claimed" || !binding.Status.ExpiresAt.After(s.currentTime()) ||
		binding.Fingerprint != configuration.Fingerprint {
		return UserSettingsStatus{}, ErrTelegramBinding
	}
	delete(s.telegramBindings, ticket)
	return s.updateTelegramSettings(actor.UserID, binding.Settings, binding.Version)
}

func (s *ConfigService) TelegramBindingNeedsRebind(settings TelegramUserSettings) bool {
	if settings.ChatID == "" {
		return false
	}
	if s == nil || s.vault == nil || s.engine == nil {
		return true
	}
	token, err := s.openSecretForPlugin(telegramPluginID, "botToken")
	parts := telegramTokenPattern.FindStringSubmatch(token)
	return err != nil || len(parts) != 2 || !settings.Verified || settings.BotID != parts[1]
}

func (s *ConfigService) TelegramCanNotify(settings TelegramUserSettings) bool {
	configuration, err := s.telegramConfiguration()
	return err == nil && settings.ChatID != "" && settings.Verified && !settings.Disabled && settings.BotID == configuration.BotID
}

func (s *ConfigService) UpdateOwnTelegramPreferences(userID int, preferences TelegramUserSettings, unbind bool, expectedVersion uint64) (UserSettingsStatus, error) {
	if _, err := s.activeNotificationUser(userID); err != nil {
		return UserSettingsStatus{}, err
	}
	current, err := s.GetOwnTelegramSettings(userID)
	if err != nil {
		return UserSettingsStatus{}, err
	}
	if current.Version != expectedVersion {
		return UserSettingsStatus{}, ErrPluginSettingConflict
	}
	settings := current.Settings
	if unbind {
		settings = TelegramUserSettings{Disabled: true, DisableLinkPreview: true}
	} else {
		if !preferences.Disabled && (settings.ChatID == "" || s.TelegramBindingNeedsRebind(settings)) {
			return UserSettingsStatus{}, ErrTelegramBinding
		}
		settings.Disabled, settings.ShowContent = preferences.Disabled, preferences.ShowContent
		settings.SpoilerContent, settings.SendAttachments = preferences.SpoilerContent, preferences.SendAttachments
		settings.DisableLinkPreview = preferences.DisableLinkPreview
	}
	return s.updateTelegramSettings(userID, settings, expectedVersion)
}

func (s *ConfigService) ReserveTelegramTest(userID int) error {
	return s.reserveTelegramAttempt(userID, "test", 30*time.Second)
}

// TelegramDeliverySettings 只向插件下发实际发信字段，不泄露绑定元数据。
func TelegramDeliverySettings(settings TelegramUserSettings) json.RawMessage {
	chatID := settings.ChatID
	if !settings.Verified || settings.Disabled {
		chatID = ""
	}
	payload, _ := json.Marshal(struct {
		ChatID             string `json:"chatId"`
		ShowContent        bool   `json:"showContent"`
		SpoilerContent     bool   `json:"spoilerContent"`
		SendAttachments    bool   `json:"sendAttachments"`
		DisableLinkPreview bool   `json:"disableLinkPreview"`
	}{chatID, settings.ShowContent, settings.SpoilerContent, settings.SendAttachments, settings.DisableLinkPreview})
	return payload
}
