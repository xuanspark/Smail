package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"xorm.io/xorm"
)

const (
	telegramPluginID                 = "telegram_push"
	userSettingsSchemaVersion uint32 = 1
	maxBulkTargets                   = 100
)

var (
	ErrPluginStoreUnavailable     = errors.New("plugin configuration store unavailable")
	ErrUnknownPluginField         = errors.New("unknown plugin configuration field")
	ErrInvalidTelegramChatID      = errors.New("invalid Telegram chat ID")
	ErrPluginSettingConflict      = errors.New("plugin setting version conflict")
	ErrPluginBulkPreviewDenied    = errors.New("plugin bulk preview denied")
	ErrPluginBulkTicketInvalid    = errors.New("plugin bulk ticket is invalid or expired")
	ErrLegacyPluginSecretConflict = errors.New("legacy plugin secret conflicts with existing secret")
)

var telegramChatIDPattern = regexp.MustCompile(`^-?[1-9][0-9]{0,18}$`)

// SecretStatus 仅描述敏感配置是否已设置，绝不包含密文或明文。
type SecretStatus struct {
	PluginID   string `json:"pluginId"`
	FieldName  string `json:"fieldName"`
	Configured bool   `json:"configured"`
	Version    uint64 `json:"version"`
}

// TelegramUserSettings 是 Telegram 插件唯一允许由用户维护的设置。
type TelegramUserSettings struct {
	ChatID             string `json:"chatId"`
	ShowContent        bool   `json:"showContent"`
	SpoilerContent     bool   `json:"spoilerContent"`
	SendAttachments    bool   `json:"sendAttachments"`
	DisableLinkPreview bool   `json:"disableLinkPreview"`
}

// TelegramUserSettingsEntry 用于在插件启动或配置更新后同步用户设置。
type TelegramUserSettingsEntry struct {
	UserID   int
	Settings TelegramUserSettings
}

// UserSettingsStatus 返回用户本人可见的设置与乐观锁版本。
type UserSettingsStatus struct {
	PluginID string               `json:"pluginId"`
	Settings TelegramUserSettings `json:"settings"`
	Version  uint64               `json:"version"`
}

// BulkPreview 是短时一次性批量执行票据的非敏感预览。
type BulkPreview struct {
	Ticket    string              `json:"ticket"`
	ExpiresAt time.Time           `json:"expiresAt"`
	Targets   []BulkPreviewTarget `json:"targets"`
}

type BulkPreviewTarget struct {
	UserID    int    `json:"userId"`
	Version   uint64 `json:"version"`
	WillWrite bool   `json:"willWrite"`
}

type pluginBulkTicket struct {
	ActorUserID int
	PluginID    string
	Settings    string
	Overwrite   bool
	Targets     []bulkTarget
	Digest      string
	ExpiresAt   time.Time
}

type bulkTarget struct {
	UserID  int
	Version uint64
}

// ConfigService 是插件全局 Secret 和用户设置的唯一写服务。
// HTTP 路由在 CG-025 完成能力接入前不得直接暴露本服务的写方法。
type ConfigService struct {
	engine    *xorm.Engine
	vault     *credential.Vault
	now       func() time.Time
	ticketsMu sync.Mutex
	tickets   map[string]pluginBulkTicket
}

func NewConfigService(engine *xorm.Engine, vault *credential.Vault) (*ConfigService, error) {
	if engine == nil || vault == nil {
		return nil, ErrPluginStoreUnavailable
	}
	return &ConfigService{
		engine:  engine,
		vault:   vault,
		now:     func() time.Time { return time.Now().UTC() },
		tickets: make(map[string]pluginBulkTicket),
	}, nil
}

// ImportLegacySecrets atomically stores compatibility-only V2 file secrets in plugin_secret.
// Existing values may only be reused when their plaintext matches the source value.
func (s *ConfigService) ImportLegacySecrets(ctx context.Context, values []config.LegacyPluginSecret) error {
	if s == nil || s.engine == nil || s.vault == nil || len(values) == 0 {
		return ErrPluginStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	entries := append([]config.LegacyPluginSecret(nil), values...)
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].PluginID == entries[right].PluginID {
			return entries[left].FieldName < entries[right].FieldName
		}
		return entries[left].PluginID < entries[right].PluginID
	})
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateSecretField(entry.PluginID, entry.FieldName); err != nil || entry.Plaintext == "" || len(entry.Plaintext) > 4096 {
			return ErrUnknownPluginField
		}
		key := entry.PluginID + "\x00" + entry.FieldName
		if _, exists := seen[key]; exists {
			return ErrLegacyPluginSecretConflict
		}
		seen[key] = struct{}{}
	}

	session := s.engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin legacy plugin secret transaction: %w", err)
	}
	rollback := func(cause error) error {
		_ = session.Rollback()
		return cause
	}
	now := s.currentTime()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		record := models.PluginSecret{}
		found, err := session.Where("plugin_id = ? AND field_name = ?", entry.PluginID, entry.FieldName).Get(&record)
		if err != nil {
			return rollback(fmt.Errorf("load legacy plugin secret: %w", err))
		}
		if found {
			plaintext, decryptErr := s.vault.Decrypt(record.KeyVersion, record.Ciphertext)
			if decryptErr != nil || plaintext != entry.Plaintext {
				return rollback(ErrLegacyPluginSecretConflict)
			}
			continue
		}
		ciphertext, encryptErr := s.vault.Encrypt(entry.Plaintext)
		if encryptErr != nil {
			return rollback(ErrPluginStoreUnavailable)
		}
		record = models.PluginSecret{
			PluginID: entry.PluginID, FieldName: entry.FieldName, Ciphertext: ciphertext, KeyVersion: s.vault.Version(),
			RecordVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err = session.Insert(&record); err != nil {
			return rollback(ErrLegacyPluginSecretConflict)
		}
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit legacy plugin secret transaction: %w", err)
	}
	return nil
}

func (s *ConfigService) GetSecretStatus(pluginID, fieldName string) (SecretStatus, error) {
	if err := validateSecretField(pluginID, fieldName); err != nil {
		return SecretStatus{}, err
	}
	record, found, err := s.findSecret(pluginID, fieldName)
	if err != nil {
		return SecretStatus{}, err
	}
	if !found {
		return SecretStatus{PluginID: pluginID, FieldName: fieldName}, nil
	}
	return SecretStatus{PluginID: pluginID, FieldName: fieldName, Configured: true, Version: record.RecordVersion}, nil
}

func (s *ConfigService) ChangeSecret(pluginID, fieldName string, change config.SecretChange, expectedVersion uint64) (SecretStatus, error) {
	if err := validateSecretField(pluginID, fieldName); err != nil {
		return SecretStatus{}, err
	}
	if s == nil || s.engine == nil || s.vault == nil {
		return SecretStatus{}, ErrPluginStoreUnavailable
	}
	current, found, err := s.findSecret(pluginID, fieldName)
	if err != nil {
		return SecretStatus{}, err
	}
	currentVersion := uint64(0)
	stored := config.StoredSecret{}
	if found {
		currentVersion = current.RecordVersion
		stored = config.StoredSecret{Ciphertext: current.Ciphertext, Version: current.KeyVersion, UpdatedAt: current.UpdatedAt}
	}
	if currentVersion != expectedVersion {
		return SecretStatus{}, ErrPluginSettingConflict
	}
	updated, err := config.ApplySecretChange(s.vault, stored, change, config.SecretPolicy{AllowClear: true, MaxLength: 4096}, s.currentTime())
	if err != nil {
		return SecretStatus{}, err
	}

	session := s.engine.NewSession()
	defer session.Close()
	if err = session.Begin(); err != nil {
		return SecretStatus{}, fmt.Errorf("begin plugin secret transaction: %w", err)
	}
	rollback := func(cause error) (SecretStatus, error) {
		_ = session.Rollback()
		return SecretStatus{}, cause
	}
	if !updated.Configured() {
		if found {
			affected, deleteErr := session.ID(current.ID).Where("record_version = ?", expectedVersion).Delete(&models.PluginSecret{})
			if deleteErr != nil {
				return rollback(fmt.Errorf("clear plugin secret: %w", deleteErr))
			}
			if affected != 1 {
				return rollback(ErrPluginSettingConflict)
			}
		}
		if err = session.Commit(); err != nil {
			return SecretStatus{}, fmt.Errorf("commit plugin secret clear: %w", err)
		}
		return SecretStatus{PluginID: pluginID, FieldName: fieldName}, nil
	}

	now := s.currentTime()
	if !found {
		record := models.PluginSecret{
			PluginID: pluginID, FieldName: fieldName, Ciphertext: updated.Ciphertext, KeyVersion: updated.Version,
			RecordVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err = session.Insert(&record); err != nil {
			return rollback(ErrPluginSettingConflict)
		}
		if err = session.Commit(); err != nil {
			return SecretStatus{}, fmt.Errorf("commit plugin secret create: %w", err)
		}
		return SecretStatus{PluginID: pluginID, FieldName: fieldName, Configured: true, Version: record.RecordVersion}, nil
	}
	newVersion := currentVersion + 1
	affected, updateErr := session.ID(current.ID).Where("record_version = ?", expectedVersion).Cols("ciphertext", "key_version", "record_version", "updated_at").Update(&models.PluginSecret{
		Ciphertext: updated.Ciphertext, KeyVersion: updated.Version, RecordVersion: newVersion, UpdatedAt: now,
	})
	if updateErr != nil {
		return rollback(fmt.Errorf("update plugin secret: %w", updateErr))
	}
	if affected != 1 {
		return rollback(ErrPluginSettingConflict)
	}
	if err = session.Commit(); err != nil {
		return SecretStatus{}, fmt.Errorf("commit plugin secret update: %w", err)
	}
	return SecretStatus{PluginID: pluginID, FieldName: fieldName, Configured: true, Version: newVersion}, nil
}

// openSecretForPlugin 仅供同进程的受控插件 DTO 组装逻辑使用，不能直接映射为 API 响应。
func (s *ConfigService) openSecretForPlugin(pluginID, fieldName string) (string, error) {
	if err := validateSecretField(pluginID, fieldName); err != nil {
		return "", err
	}
	record, found, err := s.findSecret(pluginID, fieldName)
	if err != nil {
		return "", err
	}
	if !found {
		return "", config.ErrSecretUnavailable
	}
	return config.OpenStoredSecret(s.vault, config.StoredSecret{Ciphertext: record.Ciphertext, Version: record.KeyVersion, UpdatedAt: record.UpdatedAt})
}

func (s *ConfigService) GetOwnTelegramSettings(actorUserID int) (UserSettingsStatus, error) {
	if actorUserID <= 0 {
		return UserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	return s.getTelegramSettings(actorUserID)
}

// ListTelegramUserSettings 返回已配置的 Telegram 用户设置，不返回任何密钥。
func (s *ConfigService) ListTelegramUserSettings() ([]TelegramUserSettingsEntry, error) {
	if s == nil || s.engine == nil || s.vault == nil {
		return nil, ErrPluginStoreUnavailable
	}
	var records []models.PluginUserSetting
	if err := s.engine.Where("plugin_id = ? AND enabled = ?", telegramPluginID, true).Find(&records); err != nil {
		return nil, fmt.Errorf("load Telegram user settings: %w", err)
	}
	entries := make([]TelegramUserSettingsEntry, 0, len(records))
	for _, record := range records {
		if record.SchemaVersion != userSettingsSchemaVersion {
			return nil, ErrPluginStoreUnavailable
		}
		plaintext, err := s.vault.Decrypt(credential.CurrentVersion, record.SettingsCiphertext)
		if err != nil {
			return nil, ErrPluginStoreUnavailable
		}
		fingerprint, err := s.vault.Fingerprint(plaintext)
		if err != nil || fingerprint != record.SettingsHash {
			return nil, ErrPluginStoreUnavailable
		}
		settings, err := decodeTelegramSettings(plaintext)
		if err != nil {
			return nil, ErrPluginStoreUnavailable
		}
		entries = append(entries, TelegramUserSettingsEntry{UserID: record.UserID, Settings: settings})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].UserID < entries[right].UserID })
	return entries, nil
}

// UpdateOwnTelegramSettings 的 actorUserID 必须来自已验证 Session，调用方不得传入目标用户 ID。
func (s *ConfigService) UpdateOwnTelegramSettings(actorUserID int, settings TelegramUserSettings, expectedVersion uint64) (UserSettingsStatus, error) {

	if actorUserID <= 0 {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	actor, found, err := s.findUser(actorUserID)

	if err != nil {

		return UserSettingsStatus{}, err

	}

	if !found || actor.Disabled != 0 {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	if err = access.Authorize(access.Request{ActorRole: actor.EffectiveRole(), Capability: access.CapabilityPluginWriteOwn, OwnResource: true}); err != nil {

		return UserSettingsStatus{}, err

	}

	return s.updateTelegramSettings(actorUserID, settings, expectedVersion)
}

// UpdateTelegramSettingsForUser 查询目标账户的当前角色后才进行跨用户授权。
func (s *ConfigService) UpdateTelegramSettingsForUser(actor models.User, targetUserID int, settings TelegramUserSettings, expectedVersion uint64) (UserSettingsStatus, error) {
	if actor.ID <= 0 || targetUserID <= 0 {
		return UserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	target, found, err := s.findUser(targetUserID)
	if err != nil {
		return UserSettingsStatus{}, err
	}
	if !found || target.Disabled != 0 {
		return UserSettingsStatus{}, ErrPluginStoreUnavailable
	}
	if err = access.Authorize(access.Request{
		ActorRole: actor.EffectiveRole(), Capability: access.CapabilityPluginWriteAnyUser, TargetRole: target.EffectiveRole(),
	}); err != nil {
		return UserSettingsStatus{}, err
	}
	return s.updateTelegramSettings(targetUserID, settings, expectedVersion)
}

func (s *ConfigService) PreviewBulkTelegramSettings(actor models.User, targetUserIDs []int, settings TelegramUserSettings, overwrite bool) (BulkPreview, error) {
	if actor.ID <= 0 || actor.EffectiveRole() != access.RoleSuperAdmin {
		return BulkPreview{}, ErrPluginBulkPreviewDenied
	}
	payload, err := encodeTelegramSettings(settings)
	if err != nil {
		return BulkPreview{}, err
	}
	ids, err := normalizeTargetIDs(targetUserIDs)
	if err != nil {
		return BulkPreview{}, err
	}
	targets := make([]bulkTarget, 0, len(ids))
	previewTargets := make([]BulkPreviewTarget, 0, len(ids))
	for _, userID := range ids {
		user, found, findErr := s.findUser(userID)
		if findErr != nil {
			return BulkPreview{}, findErr
		}
		if !found || user.Disabled != 0 {
			return BulkPreview{}, ErrPluginBulkPreviewDenied
		}
		if authErr := access.Authorize(access.Request{ActorRole: actor.EffectiveRole(), Capability: access.CapabilityPluginWriteAnyUser, TargetRole: user.EffectiveRole()}); authErr != nil {
			return BulkPreview{}, authErr
		}
		current, hasCurrent, getErr := s.findUserSetting(telegramPluginID, userID)
		if getErr != nil {
			return BulkPreview{}, getErr
		}
		version := uint64(0)
		if hasCurrent {
			version = current.Version
		}
		targets = append(targets, bulkTarget{UserID: userID, Version: version})
		previewTargets = append(previewTargets, BulkPreviewTarget{UserID: userID, Version: version, WillWrite: overwrite || !hasCurrent})
	}
	now := s.currentTime()
	ticket, err := randomTicket()
	if err != nil {
		return BulkPreview{}, fmt.Errorf("generate plugin bulk ticket: %w", err)
	}
	expiresAt := now.Add(5 * time.Minute)
	entry := pluginBulkTicket{ActorUserID: actor.ID, PluginID: telegramPluginID, Settings: payload, Overwrite: overwrite, Targets: targets, ExpiresAt: expiresAt}
	entry.Digest = bulkTicketDigest(entry)
	s.ticketsMu.Lock()
	s.removeExpiredTicketsLocked(now)
	s.tickets[ticket] = entry
	s.ticketsMu.Unlock()
	return BulkPreview{Ticket: ticket, ExpiresAt: expiresAt, Targets: previewTargets}, nil
}

// ExecuteBulkTelegramSettings 仅接收预览票据；目标集合、覆盖策略和版本均不能由执行请求修改。
func (s *ConfigService) ExecuteBulkTelegramSettings(actorUserID int, ticket string) error {
	if actorUserID <= 0 || strings.TrimSpace(ticket) == "" {
		return ErrPluginBulkTicketInvalid
	}
	now := s.currentTime()
	s.ticketsMu.Lock()
	s.removeExpiredTicketsLocked(now)
	entry, found := s.tickets[ticket]
	if !found || entry.ActorUserID != actorUserID || entry.ExpiresAt.Before(now) || entry.Digest != bulkTicketDigest(entry) {
		s.ticketsMu.Unlock()
		return ErrPluginBulkTicketInvalid
	}
	delete(s.tickets, ticket)
	s.ticketsMu.Unlock()

	actor, actorFound, err := s.findUser(actorUserID)

	if err != nil {

		return err

	}

	if !actorFound || actor.Disabled != 0 || actor.EffectiveRole() != access.RoleSuperAdmin {

		return ErrPluginBulkPreviewDenied

	}

	session := s.engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin plugin bulk transaction: %w", err)
	}
	rollback := func(cause error) error {
		_ = session.Rollback()
		return cause
	}
	for _, target := range entry.Targets {
		if !entry.Overwrite && target.Version != 0 {
			continue
		}
		if err := updateTelegramSettingsOnSession(session, s.vault, target.UserID, entry.Settings, target.Version, now); err != nil {
			return rollback(err)
		}
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit plugin bulk transaction: %w", err)
	}
	return nil
}

func (s *ConfigService) getTelegramSettings(userID int) (UserSettingsStatus, error) {

	record, found, err := s.findUserSetting(telegramPluginID, userID)

	if err != nil {

		return UserSettingsStatus{}, err

	}

	if !found || !record.Enabled {

		return UserSettingsStatus{PluginID: telegramPluginID}, nil

	}

	if record.SchemaVersion != userSettingsSchemaVersion {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	plaintext, err := s.vault.Decrypt(credential.CurrentVersion, record.SettingsCiphertext)

	if err != nil {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	fingerprint, err := s.vault.Fingerprint(plaintext)

	if err != nil || fingerprint != record.SettingsHash {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	settings, err := decodeTelegramSettings(plaintext)

	if err != nil {

		return UserSettingsStatus{}, ErrPluginStoreUnavailable

	}

	return UserSettingsStatus{PluginID: telegramPluginID, Settings: settings, Version: record.Version}, nil
}

func (s *ConfigService) updateTelegramSettings(userID int, settings TelegramUserSettings, expectedVersion uint64) (UserSettingsStatus, error) {

	payload, err := encodeTelegramSettings(settings)

	if err != nil {

		return UserSettingsStatus{}, err

	}

	session := s.engine.NewSession()

	defer session.Close()

	if err = session.Begin(); err != nil {

		return UserSettingsStatus{}, fmt.Errorf("begin user plugin setting transaction: %w", err)

	}

	if err = updateTelegramSettingsOnSession(session, s.vault, userID, payload, expectedVersion, s.currentTime()); err != nil {

		_ = session.Rollback()

		return UserSettingsStatus{}, err

	}

	if err = session.Commit(); err != nil {

		return UserSettingsStatus{}, fmt.Errorf("commit user plugin setting: %w", err)

	}

	return s.getTelegramSettings(userID)
}

func updateTelegramSettingsOnSession(session *xorm.Session, vault *credential.Vault, userID int, payload string, expectedVersion uint64, now time.Time) error {

	if session == nil || vault == nil {

		return ErrPluginStoreUnavailable

	}

	ciphertext, err := vault.Encrypt(payload)

	if err != nil {

		return ErrPluginStoreUnavailable

	}

	fingerprint, err := vault.Fingerprint(payload)

	if err != nil {

		return ErrPluginStoreUnavailable

	}

	current := models.PluginUserSetting{}

	found, err := session.Where("plugin_id = ? AND user_id = ?", telegramPluginID, userID).Get(&current)

	if err != nil {

		return fmt.Errorf("load user plugin setting: %w", err)

	}

	if !found {

		if expectedVersion != 0 {

			return ErrPluginSettingConflict

		}

		record := models.PluginUserSetting{PluginID: telegramPluginID, UserID: userID, SchemaVersion: userSettingsSchemaVersion, SettingsCiphertext: ciphertext, SettingsHash: fingerprint, Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now}

		if _, err = session.Insert(&record); err != nil {

			return ErrPluginSettingConflict

		}

		return nil

	}

	if current.Version != expectedVersion {

		return ErrPluginSettingConflict

	}

	affected, err := session.ID(current.ID).Where("version = ?", expectedVersion).Cols("schema_version", "settings_ciphertext", "settings_hash", "enabled", "version", "updated_at").Update(&models.PluginUserSetting{SchemaVersion: userSettingsSchemaVersion, SettingsCiphertext: ciphertext, SettingsHash: fingerprint, Enabled: true, Version: expectedVersion + 1, UpdatedAt: now})

	if err != nil {

		return fmt.Errorf("update user plugin setting: %w", err)

	}

	if affected != 1 {

		return ErrPluginSettingConflict

	}

	return nil
}

func (s *ConfigService) OpenSecretForPlugin(pluginID, fieldName string) (string, error) {
	return s.openSecretForPlugin(pluginID, fieldName)
}

func (s *ConfigService) findSecret(pluginID, fieldName string) (models.PluginSecret, bool, error) {
	if s == nil || s.engine == nil {
		return models.PluginSecret{}, false, ErrPluginStoreUnavailable
	}
	record := models.PluginSecret{}
	found, err := s.engine.Where("plugin_id = ? AND field_name = ?", pluginID, fieldName).Get(&record)
	if err != nil {
		return models.PluginSecret{}, false, fmt.Errorf("load plugin secret: %w", err)
	}
	return record, found, nil
}

func (s *ConfigService) findUserSetting(pluginID string, userID int) (models.PluginUserSetting, bool, error) {
	if s == nil || s.engine == nil {
		return models.PluginUserSetting{}, false, ErrPluginStoreUnavailable
	}
	record := models.PluginUserSetting{}
	found, err := s.engine.Where("plugin_id = ? AND user_id = ?", pluginID, userID).Get(&record)
	if err != nil {
		return models.PluginUserSetting{}, false, fmt.Errorf("load user plugin setting: %w", err)
	}
	return record, found, nil
}

func (s *ConfigService) findUser(userID int) (models.User, bool, error) {
	user := models.User{}
	found, err := s.engine.ID(userID).Get(&user)
	if err != nil {
		return models.User{}, false, fmt.Errorf("load plugin target user: %w", err)
	}
	return user, found, nil
}

func (s *ConfigService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func validateSecretField(pluginID, fieldName string) error {
	if _, ok := Lookup(pluginID); !ok {
		return ErrUnknownPluginID
	}
	switch pluginID {
	case "wechat_push":
		if fieldName == "appSecret" {
			return nil
		}
	case telegramPluginID:
		if fieldName == "botToken" {
			return nil
		}
	}
	return ErrUnknownPluginField
}

func ValidateTelegramUserSettings(settings TelegramUserSettings) error {
	_, err := encodeTelegramSettings(settings)
	return err
}
func encodeTelegramSettings(settings TelegramUserSettings) (string, error) {
	settings.ChatID = strings.TrimSpace(settings.ChatID)
	if settings.ChatID != "" && !telegramChatIDPattern.MatchString(settings.ChatID) {
		return "", ErrInvalidTelegramChatID
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeTelegramSettings(raw string) (TelegramUserSettings, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var settings TelegramUserSettings
	if err := decoder.Decode(&settings); err != nil {
		return TelegramUserSettings{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return TelegramUserSettings{}, ErrInvalidTelegramChatID
	}
	_, err := encodeTelegramSettings(settings)
	return settings, err
}

func normalizeTargetIDs(values []int) ([]int, error) {
	if len(values) == 0 || len(values) > maxBulkTargets {
		return nil, ErrPluginBulkPreviewDenied
	}
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, ErrPluginBulkPreviewDenied
		}
		if _, exists := seen[value]; exists {
			return nil, ErrPluginBulkPreviewDenied
		}
		seen[value] = struct{}{}
	}
	result := append([]int(nil), values...)
	sort.Ints(result)
	return result, nil
}

func randomTicket() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func bulkTicketDigest(entry pluginBulkTicket) string {
	payload := struct {
		ActorUserID int
		PluginID    string
		Settings    string
		Overwrite   bool
		Targets     []bulkTarget
		ExpiresAt   time.Time
	}{entry.ActorUserID, entry.PluginID, entry.Settings, entry.Overwrite, entry.Targets, entry.ExpiresAt}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (s *ConfigService) removeExpiredTicketsLocked(now time.Time) {
	for ticket, entry := range s.tickets {
		if !entry.ExpiresAt.After(now) {
			delete(s.tickets, ticket)
		}
	}
}
