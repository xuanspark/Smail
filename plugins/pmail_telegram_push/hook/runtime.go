package hook

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
)

const (
	pluginID           = "telegram_push"
	maxRuntimeSettings = 64 << 10
	defaultTimeout     = 30
	maxTimeout         = 120
)

var chatIDPattern = regexp.MustCompile(`^-?[1-9][0-9]{0,18}$`)

type pluginRuntimeConfig struct {
	PluginID     string                `json:"pluginId"`
	Generation   uint64                `json:"generation"`
	Settings     json.RawMessage       `json:"settings,omitempty"`
	Secrets      map[string]string     `json:"secrets,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty"`
	Network      []pluginNetworkTarget `json:"network,omitempty"`
}

type pluginNetworkTarget struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type pluginSettings struct {
	Proxy        string `json:"proxy,omitempty"`
	Timeout      int    `json:"timeout,omitempty"`
	Debug        bool   `json:"debug,omitempty"`
	WebDomain    string `json:"webDomain,omitempty"`
	HTTPSEnabled bool   `json:"httpsEnabled,omitempty"`
}

type settingStore struct {
	mu       sync.RWMutex
	settings map[int]telegramSetting
}

type telegramSetting struct {
	ChatID             string `json:"chat_id"`
	ShowContent        bool   `json:"show_content"`
	SpoilerContent     bool   `json:"spoiler_content"`
	SendAttachments    bool   `json:"send_attachments"`
	DisableLinkPreview bool   `json:"disable_link_preview"`
}

type telegramUserSettingsPayload struct {
	ChatID             string `json:"chatId"`
	ShowContent        bool   `json:"showContent"`
	SpoilerContent     bool   `json:"spoilerContent"`
	SendAttachments    bool   `json:"sendAttachments"`
	DisableLinkPreview bool   `json:"disableLinkPreview"`
}

func (p telegramUserSettingsPayload) setting() telegramSetting {
	return telegramSetting{
		ChatID:             strings.TrimSpace(p.ChatID),
		ShowContent:        p.ShowContent,
		SpoilerContent:     p.SpoilerContent,
		SendAttachments:    p.SendAttachments,
		DisableLinkPreview: p.DisableLinkPreview,
	}
}

func decodeTelegramUserSettings(data json.RawMessage) (telegramUserSettingsPayload, error) {
	var payload telegramUserSettingsPayload
	if err := decodeSingleJSON(data, &payload); err != nil {
		return telegramUserSettingsPayload{}, err
	}
	setting := payload.setting()
	if setting.ChatID != "" && !chatIDPattern.MatchString(setting.ChatID) {
		return telegramUserSettingsPayload{}, errors.New("Telegram 聊天 ID 格式不正确")
	}
	return payload, nil
}

func (h *PmailTelegramPushHook) ConfigureUserSettings(userID int, settings json.RawMessage) error {
	if userID <= 0 {
		return errors.New("用户 ID 无效")
	}
	payload, err := decodeTelegramUserSettings(settings)
	if err != nil {
		return err
	}
	return h.settings.update(userID, payload.setting())
}
func newSettingStore() *settingStore {
	return &settingStore{settings: make(map[int]telegramSetting)}
}

func defaultTelegramSetting() telegramSetting {
	return telegramSetting{
		ShowContent:        true,
		SpoilerContent:     true,
		SendAttachments:    true,
		DisableLinkPreview: true,
	}
}

func (s *settingStore) get(userID int) telegramSetting {
	s.mu.RLock()
	setting, ok := s.settings[userID]
	s.mu.RUnlock()
	if !ok {
		return defaultTelegramSetting()
	}
	return setting
}

func (s *settingStore) update(userID int, setting telegramSetting) error {
	if userID <= 0 || !validChatID(setting.ChatID) {
		return errors.New("Chat ID 格式不正确")
	}
	setting.ChatID = strings.TrimSpace(setting.ChatID)
	s.mu.Lock()
	s.settings[userID] = setting
	s.mu.Unlock()
	return nil
}

func validChatID(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || chatIDPattern.MatchString(value)
}

func parseRuntimeConfig(data json.RawMessage) (pluginRuntimeConfig, pluginSettings, string, error) {
	if len(data) == 0 || len(data) > maxRuntimeSettings {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("插件运行时配置无效")
	}
	var runtime pluginRuntimeConfig
	if err := decodeSingleJSON(data, &runtime); err != nil {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("插件运行时配置无效")
	}
	if runtime.PluginID != pluginID {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("插件 ID 不匹配")
	}
	if runtime.Generation == 0 {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 插件配置代次无效")
	}
	if !hasOnlyCapability(runtime.Capabilities, "network") {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 插件能力权限无效")
	}
	var settings pluginSettings
	if len(runtime.Settings) > 0 {
		if err := decodeSingleJSON(runtime.Settings, &settings); err != nil {
			return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 插件配置无效")
		}
	}
	if settings.Timeout == 0 {
		settings.Timeout = defaultTimeout
	}
	if settings.Timeout < 1 || settings.Timeout > maxTimeout {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 请求超时时间无效")
	}
	if settings.Proxy != "" {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 插件不允许使用未授权代理")
	}
	secret := strings.TrimSpace(runtime.Secrets["botToken"])
	if secret == "" {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram Bot Token 未配置")
	}
	if len(runtime.Secrets) != 1 {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 插件密钥字段无效")
	}
	if !hasOnlyTelegramNetworkTarget(runtime.Network) {
		return pluginRuntimeConfig{}, pluginSettings{}, "", errors.New("Telegram 网络权限未配置")
	}
	return runtime, settings, secret, nil
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func hasOnlyCapability(capabilities []string, expected string) bool {
	return len(capabilities) == 1 && capabilities[0] == expected
}

func hasOnlyTelegramNetworkTarget(targets []pluginNetworkTarget) bool {
	return len(targets) == 1 && strings.EqualFold(targets[0].Scheme, "https") && strings.EqualFold(targets[0].Host, "api.telegram.org") && targets[0].Port == 443
}
