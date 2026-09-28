package plugin

import (
	"encoding/json"
	"errors"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

const weChatPluginID = "wechat_push"

var ErrWeChatBinding = errors.New("微信绑定已失效，请重新扫码")

// WeChatNotice 仅承载可公开的固定业务提示，数据库和外部请求错误不得使用此类型。
type WeChatNotice string

func (notice WeChatNotice) Error() string { return string(notice) }

// WeChatUserSettings 只通过已验证的扫码流程写入接收身份，不接受页面直接指定 OpenID。
type WeChatUserSettings struct {
	AppID       string `json:"appId"`
	OpenID      string `json:"openId"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	ShowContent bool   `json:"showContent"`
}

type WeChatUserSettingsStatus struct {
	Settings    WeChatUserSettings
	Version     uint64
	NeedsRebind bool
}

type WeChatUserSettingsEntry struct {
	UserID   int
	Settings WeChatUserSettings
}

func (s *ConfigService) activeNotificationUser(userID int) (models.User, error) {
	if s == nil || s.engine == nil || s.vault == nil || userID <= 0 {
		return models.User{}, ErrPluginStoreUnavailable
	}
	user, found, err := s.findUser(userID)
	if err != nil || !found || user.Disabled != 0 {
		return models.User{}, ErrPluginStoreUnavailable
	}
	if err := access.Authorize(access.Request{ActorRole: user.EffectiveRole(), Capability: access.CapabilityPluginWriteOwn, OwnResource: true}); err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (s *ConfigService) GetOwnWeChatSettings(userID int) (WeChatUserSettingsStatus, error) {
	if _, err := s.activeNotificationUser(userID); err != nil {
		return WeChatUserSettingsStatus{}, err
	}
	record, found, err := s.findUserSetting(weChatPluginID, userID)
	if err != nil || !found {
		return WeChatUserSettingsStatus{}, err
	}
	settings, err := s.decodeWeChatSettings(record)
	return WeChatUserSettingsStatus{Settings: settings, Version: record.Version,
		NeedsRebind: weChatBindingNeedsRebind(settings, currentWeChatAppID())}, err
}

func currentWeChatAppID() string {
	manager := config.CurrentManager()
	if manager == nil {
		return ""
	}
	instance, found := manager.Snapshot().Plugins.Plugins[weChatPluginID]
	var settings struct {
		AppID string `json:"appId"`
	}
	if !found || json.Unmarshal(instance.Settings, &settings) != nil {
		return ""
	}
	return settings.AppID
}

func weChatBindingNeedsRebind(settings WeChatUserSettings, appID string) bool {
	return settings.OpenID != "" && (!validWeChatIdentity(appID) || settings.AppID != appID)
}

func (s *ConfigService) decodeWeChatSettings(record models.PluginUserSetting) (WeChatUserSettings, error) {
	var settings WeChatUserSettings
	if record.SchemaVersion != userSettingsSchemaVersion {
		return settings, ErrPluginStoreUnavailable
	}
	plaintext, err := s.vault.Decrypt(credential.CurrentVersion, record.SettingsCiphertext)
	if err != nil {
		return settings, ErrPluginStoreUnavailable
	}
	fingerprint, err := s.vault.Fingerprint(plaintext)
	if err != nil || fingerprint != record.SettingsHash || !json.Valid([]byte(plaintext)) {
		return settings, ErrPluginStoreUnavailable
	}
	decoder := json.NewDecoder(strings.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil || !validWeChatSettings(settings) {
		return WeChatUserSettings{}, ErrPluginStoreUnavailable
	}
	return settings, nil
}

func validWeChatIdentity(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func validWeChatSettings(settings WeChatUserSettings) bool {
	if len(settings.DisplayName) > 256 {
		return false
	}
	if settings.OpenID == "" {
		return settings.AppID == "" && !settings.Enabled
	}
	return validWeChatIdentity(settings.OpenID) && validWeChatIdentity(settings.AppID)
}

func (s *ConfigService) saveWeChatSettings(userID int, settings WeChatUserSettings, expectedVersion uint64) (WeChatUserSettingsStatus, error) {
	if _, err := s.activeNotificationUser(userID); err != nil {
		return WeChatUserSettingsStatus{}, err
	}
	if !validWeChatSettings(settings) {
		return WeChatUserSettingsStatus{}, ErrWeChatBinding
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return WeChatUserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	ciphertext, err := s.vault.Encrypt(string(payload))
	if err != nil {
		return WeChatUserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	fingerprint, err := s.vault.Fingerprint(string(payload))
	if err != nil {
		return WeChatUserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	current, found, err := s.findUserSetting(weChatPluginID, userID)
	if err != nil {
		return WeChatUserSettingsStatus{}, err
	}
	if current.Version != expectedVersion || !found && expectedVersion != 0 {
		return WeChatUserSettingsStatus{}, ErrPluginSettingConflict
	}
	now := s.currentTime()
	record := models.PluginUserSetting{PluginID: weChatPluginID, UserID: userID, SchemaVersion: userSettingsSchemaVersion,
		SettingsCiphertext: ciphertext, SettingsHash: fingerprint, Enabled: true, Version: expectedVersion + 1, CreatedAt: now, UpdatedAt: now}
	if !found {
		if _, err := s.engine.Insert(&record); err != nil {
			return WeChatUserSettingsStatus{}, ErrPluginSettingConflict
		}
	} else {
		changed, err := s.engine.ID(current.ID).Where("version = ?", expectedVersion).
			Cols("schema_version", "settings_ciphertext", "settings_hash", "enabled", "version", "updated_at").Update(&record)
		if err != nil || changed != 1 {
			return WeChatUserSettingsStatus{}, ErrPluginSettingConflict
		}
	}
	return WeChatUserSettingsStatus{Settings: settings, Version: record.Version,
		NeedsRebind: weChatBindingNeedsRebind(settings, currentWeChatAppID())}, nil
}

func (s *ConfigService) UpdateOwnWeChatPreferences(userID int, enabled, showContent, unbind bool, expectedVersion uint64) (WeChatUserSettingsStatus, error) {
	current, err := s.GetOwnWeChatSettings(userID)
	if err != nil {
		return current, err
	}
	settings := current.Settings
	if !unbind && enabled && current.NeedsRebind {
		return current, ErrWeChatBinding
	}
	if unbind {
		settings = WeChatUserSettings{}
	} else {
		settings.Enabled, settings.ShowContent = enabled, showContent
	}
	return s.saveWeChatSettings(userID, settings, expectedVersion)
}

func (s *ConfigService) ListWeChatUserSettings() ([]WeChatUserSettingsEntry, error) {
	if s == nil || s.engine == nil || s.vault == nil {
		return nil, ErrPluginStoreUnavailable
	}
	var records []models.PluginUserSetting
	if err := s.engine.Where("plugin_id = ? AND enabled = ?", weChatPluginID, true).Find(&records); err != nil {
		return nil, ErrPluginStoreUnavailable
	}
	entries := make([]WeChatUserSettingsEntry, 0, len(records))
	for _, record := range records {
		user, found, err := s.findUser(record.UserID)
		if err != nil {
			return nil, ErrPluginStoreUnavailable
		}
		if !found || user.Disabled != 0 {
			continue
		}
		settings, err := s.decodeWeChatSettings(record)
		if err != nil {
			return nil, err
		}
		entries = append(entries, WeChatUserSettingsEntry{UserID: record.UserID, Settings: settings})
	}
	return entries, nil
}
