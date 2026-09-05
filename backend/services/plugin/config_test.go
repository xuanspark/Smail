package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/credential"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestConfigServiceStoresPluginSecretsAsCiphertext(t *testing.T) {
	service, engine := newPluginConfigService(t)
	status, err := service.ChangeSecret("telegram_push", "botToken", config.SecretChange{
		Operation: config.SecretReplace,
		Value:     &config.SecretInput{Plaintext: "telegram-token-for-test"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Version != 1 {
		t.Fatalf("status = %+v", status)
	}
	if _, err = service.ChangeSecret("telegram_push", "unknown", config.SecretChange{}, 0); !errors.Is(err, ErrUnknownPluginField) {
		t.Fatalf("unknown field error = %v", err)
	}

	record := models.PluginSecret{}
	found, err := engine.Where("plugin_id = ? AND field_name = ?", "telegram_push", "botToken").Get(&record)
	if err != nil || !found {
		t.Fatalf("load secret record: found=%v err=%v", found, err)
	}
	if record.Ciphertext == "telegram-token-for-test" || record.Ciphertext == "" || record.KeyVersion == 0 {
		t.Fatalf("secret is not safely stored: %+v", record)
	}
	visible, err := service.GetSecretStatus("telegram_push", "botToken")
	if err != nil || !visible.Configured || visible.Version != record.RecordVersion {
		t.Fatalf("visible status = %+v, err=%v", visible, err)
	}
	if _, err = engine.ID(record.ID).Cols("ciphertext").Update(&models.PluginSecret{Ciphertext: "smail:v1:tampered"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.openSecretForPlugin("telegram_push", "botToken"); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestConfigServiceEncryptsTelegramUserSettingsAndRejectsTampering(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "user", access.RoleUser)
	status, err := service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{ChatID: "-100123456"}, 0)
	if err != nil || status.Version != 1 {
		t.Fatalf("update status = %+v, err=%v", status, err)
	}
	record := models.PluginUserSetting{}
	found, err := engine.Where("plugin_id = ? AND user_id = ?", telegramPluginID, user.ID).Get(&record)
	if err != nil || !found {
		t.Fatalf("load user setting: found=%v err=%v", found, err)
	}
	if record.SettingsCiphertext == `{"chatId":"-100123456"}` || record.SettingsCiphertext == "" || record.SettingsHash == "" || !record.Enabled || record.SchemaVersion != userSettingsSchemaVersion {
		t.Fatalf("user setting is not safely stored: %+v", record)
	}
	if _, err = engine.ID(record.ID).Cols("settings_hash").Update(&models.PluginUserSetting{SettingsHash: "tampered"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetOwnTelegramSettings(user.ID); !errors.Is(err, ErrPluginStoreUnavailable) {
		t.Fatalf("tampered user setting error = %v", err)
	}
}
func TestConfigServiceAllowsEmptyTelegramChatIDToDisablePush(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "disabled-push", access.RoleUser)

	status, err := service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{}, 0)
	if err != nil {
		t.Fatalf("empty chat ID update failed: %v", err)
	}
	if status.Settings.ChatID != "" || status.Version != 1 {
		t.Fatalf("unexpected empty chat ID status: %+v", status)
	}
	entries, err := service.ListTelegramUserSettings()
	if err != nil || len(entries) != 1 || entries[0].Settings.ChatID != "" {
		t.Fatalf("stored empty chat ID settings = %+v, err=%v", entries, err)
	}
}

func TestConfigServiceRejectsTrailingTelegramSettingsJSON(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "trailing-json", access.RoleUser)
	if _, err := service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{ChatID: "123"}, 0); err != nil {
		t.Fatal(err)
	}
	record := models.PluginUserSetting{}
	found, err := engine.Where("plugin_id = ? AND user_id = ?", telegramPluginID, user.ID).Get(&record)
	if err != nil || !found {
		t.Fatalf("load setting record: found=%v err=%v", found, err)
	}
	plaintext, err := service.vault.Decrypt(credential.CurrentVersion, record.SettingsCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	badPlaintext := plaintext + " {}"
	ciphertext, err := service.vault.Encrypt(badPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := service.vault.Fingerprint(badPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.ID(record.ID).Cols("settings_ciphertext", "settings_hash").Update(&models.PluginUserSetting{
		SettingsCiphertext: ciphertext,
		SettingsHash:       fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetOwnTelegramSettings(user.ID); !errors.Is(err, ErrPluginStoreUnavailable) {
		t.Fatalf("trailing JSON was accepted: %v", err)
	}
}

func TestConfigServiceEnforcesUserSettingRolesAndVersions(t *testing.T) {
	service, engine := newPluginConfigService(t)
	super := insertPluginTestUser(t, engine, "root", access.RoleSuperAdmin)
	admin := insertPluginTestUser(t, engine, "admin", access.RoleAdmin)
	secondAdmin := insertPluginTestUser(t, engine, "admin2", access.RoleAdmin)
	user := insertPluginTestUser(t, engine, "user", access.RoleUser)

	own, err := service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{ChatID: "123456"}, 0)
	if err != nil || own.Version != 1 {
		t.Fatalf("own update = %+v, err=%v", own, err)
	}
	updated, err := service.UpdateTelegramSettingsForUser(admin, user.ID, TelegramUserSettings{ChatID: "-100123456"}, own.Version)
	if err != nil || updated.Version != 2 {
		t.Fatalf("admin update = %+v, err=%v", updated, err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(admin, secondAdmin.ID, TelegramUserSettings{ChatID: "1"}, 0); !access.IsDenied(err, access.CodeTargetRoleDenied) {
		t.Fatalf("admin writes admin error = %v", err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(user, admin.ID, TelegramUserSettings{ChatID: "1"}, 0); !access.IsDenied(err, access.CodeAccessDenied) {
		t.Fatalf("user cross-account error = %v", err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(super, secondAdmin.ID, TelegramUserSettings{ChatID: "2"}, 0); err != nil {
		t.Fatalf("super update admin: %v", err)
	}
	if _, err = service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{ChatID: "777"}, own.Version); !errors.Is(err, ErrPluginSettingConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	if _, err = service.UpdateOwnTelegramSettings(user.ID, TelegramUserSettings{ChatID: "001"}, updated.Version); !errors.Is(err, ErrInvalidTelegramChatID) {
		t.Fatalf("invalid chat ID error = %v", err)
	}
}

func TestConfigServiceBulkTicketIsBoundOneTimeAndVersioned(t *testing.T) {
	service, engine := newPluginConfigService(t)
	super := insertPluginTestUser(t, engine, "root", access.RoleSuperAdmin)
	first := insertPluginTestUser(t, engine, "first", access.RoleUser)
	second := insertPluginTestUser(t, engine, "second", access.RoleUser)

	preview, err := service.PreviewBulkTelegramSettings(super, []int{second.ID, first.ID}, TelegramUserSettings{ChatID: "123"}, false)
	if err != nil || len(preview.Targets) != 2 || preview.Targets[0].UserID != first.ID {
		t.Fatalf("preview = %+v, err=%v", preview, err)
	}
	if err = service.ExecuteBulkTelegramSettings(super.ID, preview.Ticket); err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteBulkTelegramSettings(super.ID, preview.Ticket); !errors.Is(err, ErrPluginBulkTicketInvalid) {
		t.Fatalf("ticket replay error = %v", err)
	}
	firstStatus, err := service.GetOwnTelegramSettings(first.ID)
	if err != nil || firstStatus.Version != 1 || firstStatus.Settings.ChatID != "123" {
		t.Fatalf("first setting = %+v, err=%v", firstStatus, err)
	}

	conflicting, err := service.PreviewBulkTelegramSettings(super, []int{first.ID}, TelegramUserSettings{ChatID: "456"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateOwnTelegramSettings(first.ID, TelegramUserSettings{ChatID: "789"}, firstStatus.Version); err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteBulkTelegramSettings(super.ID, conflicting.Ticket); !errors.Is(err, ErrPluginSettingConflict) {
		t.Fatalf("bulk version conflict error = %v", err)
	}
	if err = service.ExecuteBulkTelegramSettings(first.ID, conflicting.Ticket); !errors.Is(err, ErrPluginBulkTicketInvalid) {
		t.Fatalf("actor-bound ticket error = %v", err)
	}

	base := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	expiring, err := service.PreviewBulkTelegramSettings(super, []int{second.ID}, TelegramUserSettings{ChatID: "999"}, true)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return base.Add(6 * time.Minute) }
	if err = service.ExecuteBulkTelegramSettings(super.ID, expiring.Ticket); !errors.Is(err, ErrPluginBulkTicketInvalid) {
		t.Fatalf("expired ticket error = %v", err)
	}
}

func newPluginConfigService(t *testing.T) (*ConfigService, *xorm.Engine) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "plugin-config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	runner := migration.Runner{DB: engine.DB().DB, Dialect: migration.DialectSQLite, Migrations: migration.DefaultMigrations()}
	if _, err = runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewConfigService(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	return service, engine
}

func insertPluginTestUser(t *testing.T, engine *xorm.Engine, account string, role access.Role) models.User {
	t.Helper()
	user := models.User{Account: account, Name: account, Password: "hash"}
	user.ApplyRole(role)
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	return user
}
