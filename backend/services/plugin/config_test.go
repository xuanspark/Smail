package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
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
	status, err := service.updateTelegramSettings(user.ID, TelegramUserSettings{ChatID: "-100123456"}, 0)
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

func TestEveryRoleKeepsIndependentOwnWeChatSettings(t *testing.T) {
	service, engine := newPluginConfigService(t)
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			user := insertPluginTestUser(t, engine, "wechat-"+string(role), role)
			status, err := service.GetOwnWeChatSettings(user.ID)
			if err != nil || status.Settings.OpenID != "" || status.Version != 0 {
				t.Fatalf("角色=%s，不应自动继承系统接收人：%v", role, err)
			}
			if _, err := service.UpdateOwnWeChatPreferences(user.ID, true, false, false, 0); !errors.Is(err, ErrWeChatBinding) {
				t.Fatal("未绑定身份时不应直接启用微信推送")
			}
			settings := WeChatUserSettings{AppID: "wx-test-application", OpenID: "openid-" + string(role), Enabled: true}
			status, err = service.saveWeChatSettings(user.ID, settings, 0)
			if err != nil || status.Settings != settings || status.Version != 1 {
				t.Fatalf("角色=%s，保存独立接收人失败：%v", role, err)
			}
			record, found, err := service.findUserSetting(weChatPluginID, user.ID)
			if err != nil || !found || record.SettingsCiphertext == "" || record.SettingsCiphertext == settings.OpenID {
				t.Fatal("微信个人身份必须加密保存")
			}
			if _, err := service.UpdateOwnWeChatPreferences(user.ID, false, false, false, 0); !errors.Is(err, ErrPluginSettingConflict) {
				t.Fatal("旧版本不应覆盖绑定设置")
			}
			status, err = service.UpdateOwnWeChatPreferences(user.ID, false, false, false, 1)
			if err != nil || status.Settings.OpenID != settings.OpenID || status.Settings.Enabled {
				t.Fatal("停用推送应保留本人绑定")
			}
			status, err = service.UpdateOwnWeChatPreferences(user.ID, false, false, true, 2)
			if err != nil || status.Settings.OpenID != "" || status.Settings.Enabled || status.Version != 3 {
				t.Fatal("解绑应清除本人接收身份")
			}
		})
	}
}

func TestWeChatBindingIdentityMustMatchCurrentApplication(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings WeChatUserSettings
		appID    string
		want     bool
	}{
		{name: "unbound"},
		{name: "same-app", settings: WeChatUserSettings{AppID: "wx-original-app", OpenID: "openid-test-user", Enabled: true}, appID: "wx-original-app"},
		{name: "changed-app", settings: WeChatUserSettings{AppID: "wx-original-app", OpenID: "openid-test-user", Enabled: true}, appID: "wx-replacement-app", want: true},
		{name: "missing-app", settings: WeChatUserSettings{AppID: "wx-original-app", OpenID: "openid-test-user"}, want: true},
		{name: "invalid-app", settings: WeChatUserSettings{AppID: "wx-original-app", OpenID: "openid-test-user"}, appID: "invalid", want: true},
		{name: "disabled-same-app", settings: WeChatUserSettings{AppID: "wx-original-app", OpenID: "openid-test-user"}, appID: "wx-original-app"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := weChatBindingNeedsRebind(test.settings, test.appID); got != test.want {
				t.Fatal(got, test.want)
			}
		})
	}
}

func TestWeChatBindingStatusRejectsOtherSessionsAndRevokedAccounts(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "wechat-binding", access.RoleSuperAdmin)
	other := insertPluginTestUser(t, engine, "wechat-other", access.RoleUser)
	actor := NotificationBindingActor{UserID: user.ID, SessionVersion: user.SessionVersion,
		SecurityVersion: user.SecurityVersion, SessionHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	service.wechatBindings = map[string]weChatBinding{"ticket": {Actor: actor, Status: WeChatBindingStatus{Phase: "waiting", ExpiresAt: now.Add(time.Minute)}}}
	if _, err := service.GetWeChatBinding(actor, "ticket"); err != nil {
		t.Fatalf("本人会话应能读取绑定状态：%v", err)
	}
	otherActor := actor
	otherActor.UserID = other.ID
	if _, err := service.GetWeChatBinding(otherActor, "ticket"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatal("其他账户不应读取绑定请求")
	}
	otherActor = actor
	otherActor.SessionHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if _, err := service.GetWeChatBinding(otherActor, "ticket"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatal("同一账户的其他会话不应接管绑定")
	}
	now = now.Add(2 * time.Minute)
	if _, err := service.GetWeChatBinding(actor, "ticket"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatal("过期绑定不应继续使用")
	}
	now = now.Add(-2 * time.Minute)
	if _, err := engine.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetWeChatBinding(actor, "ticket"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatal("被禁用账户的绑定必须立即失效")
	}
}

func TestWeChatTestMessagesAreRateLimitedPerAccount(t *testing.T) {
	service, engine := newPluginConfigService(t)
	first := insertPluginTestUser(t, engine, "wechat-rate-first", access.RoleUser)
	second := insertPluginTestUser(t, engine, "wechat-rate-second", access.RoleUser)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	if service.ReserveWeChatTest(first.ID) != nil || service.ReserveWeChatTest(second.ID) != nil {
		t.Fatal("不同账户首次测试应相互独立")
	}
	if service.ReserveWeChatTest(first.ID) == nil {
		t.Fatal("短时间内不应重复发送测试消息")
	}
	now = now.Add(30 * time.Second)
	if err := service.ReserveWeChatTest(first.ID); err != nil {
		t.Fatalf("冷却结束后应允许重试：%v", err)
	}
}

func TestWeChatBindingCancellationAllowsExpiredAndMissingTickets(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "wechat-cancel", access.RoleSuperAdmin)
	actor := NotificationBindingActor{UserID: user.ID, SessionHash: strings.Repeat("a", 64),
		SessionVersion: user.SessionVersion, SecurityVersion: user.SecurityVersion}
	service.wechatBindings = map[string]weChatBinding{
		"expired": {Actor: actor, Status: WeChatBindingStatus{ExpiresAt: time.Now().Add(-time.Minute)}},
	}
	otherActor := actor
	otherActor.SessionHash = strings.Repeat("b", 64)
	if err := service.CancelWeChatBinding(otherActor, "expired"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatalf("不得撤销其他会话的绑定：%v", err)
	}
	if len(service.wechatBindings) != 1 {
		t.Fatal("其他会话的取消请求不应删除原绑定")
	}
	if err := service.CancelWeChatBinding(actor, "expired"); err != nil {
		t.Fatalf("过期请求应允许取消：%v", err)
	}
	if len(service.wechatBindings) != 0 {
		t.Fatal("已取消的绑定不应残留")
	}
	if err := service.CancelWeChatBinding(actor, "expired"); err != nil {
		t.Fatalf("重复取消应保持幂等：%v", err)
	}
	if err := service.CancelWeChatBinding(actor, "missing"); err != nil {
		t.Fatalf("不存在的请求应允许关闭：%v", err)
	}
	actor.SecurityVersion++
	if err := service.CancelWeChatBinding(actor, "missing"); !errors.Is(err, ErrWeChatBinding) {
		t.Fatalf("幂等取消仍必须验证当前账户和会话：%v", err)
	}
}

func TestEveryRoleKeepsIndependentOwnTelegramSettings(t *testing.T) {
	service, engine := newPluginConfigService(t)
	var users []models.User
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		user := insertPluginTestUser(t, engine, "own-"+string(role), role)
		users = append(users, user)
		status, err := service.GetOwnTelegramSettings(user.ID)
		if err != nil || status.Settings.ChatID != "" || status.Version != 0 {
			t.Fatalf("角色=%s，不应自动绑定系统接收身份：%v", role, err)
		}
	}
	chatIDs := []string{"10101", "20202", "30303"}
	for index, user := range users {
		status, err := service.updateTelegramSettings(user.ID, TelegramUserSettings{ChatID: chatIDs[index]}, 0)
		if err != nil || status.Settings.ChatID != chatIDs[index] || status.Version != 1 {
			t.Fatalf("角色=%s，本人通知设置保存失败：%v", user.EffectiveRole(), err)
		}
	}
	if _, err := service.UpdateOwnTelegramSettings(users[0].ID, TelegramUserSettings{ChatID: "40404"}, 1); !errors.Is(err, ErrTelegramBinding) {
		t.Fatalf("超级管理员不能修改本人的通知设置：%v", err)
	}
	for index, user := range users {
		status, err := service.GetOwnTelegramSettings(user.ID)
		if err != nil || status.Settings.ChatID != chatIDs[index] {
			t.Fatalf("角色=%s，个人通知设置发生跨账户串用：%v", user.EffectiveRole(), err)
		}
	}
}

func TestConfigServiceRejectsTrailingTelegramSettingsJSON(t *testing.T) {
	service, engine := newPluginConfigService(t)
	user := insertPluginTestUser(t, engine, "trailing-json", access.RoleUser)
	if _, err := service.updateTelegramSettings(user.ID, TelegramUserSettings{ChatID: "123"}, 0); err != nil {
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

	own, err := service.updateTelegramSettings(user.ID, TelegramUserSettings{ChatID: "123456"}, 0)
	if err != nil || own.Version != 1 {
		t.Fatalf("own update = %+v, err=%v", own, err)
	}
	updated, err := service.UpdateTelegramSettingsForUser(admin, user.ID, TelegramUserSettings{Disabled: true}, own.Version)
	if err != nil || updated.Version != 2 {
		t.Fatalf("admin update = %+v, err=%v", updated, err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(admin, secondAdmin.ID, TelegramUserSettings{Disabled: true}, 0); !access.IsDenied(err, access.CodeTargetRoleDenied) {
		t.Fatalf("admin writes admin error = %v", err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(user, admin.ID, TelegramUserSettings{Disabled: true}, 0); !access.IsDenied(err, access.CodeAccessDenied) {
		t.Fatalf("user cross-account error = %v", err)
	}
	if _, err = service.UpdateTelegramSettingsForUser(super, secondAdmin.ID, TelegramUserSettings{Disabled: true}, 0); err != nil {
		t.Fatalf("super update admin: %v", err)
	}
	if _, err = service.UpdateOwnTelegramPreferences(user.ID, TelegramUserSettings{Disabled: true}, false, own.Version); !errors.Is(err, ErrPluginSettingConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	if err = ValidateTelegramUserSettings(TelegramUserSettings{ChatID: "001"}); !errors.Is(err, ErrInvalidTelegramChatID) {
		t.Fatalf("invalid chat ID error = %v", err)
	}
}

func TestConfigServiceBulkTicketIsBoundOneTimeAndVersioned(t *testing.T) {
	service, engine := newPluginConfigService(t)
	super := insertPluginTestUser(t, engine, "root", access.RoleSuperAdmin)
	first := insertPluginTestUser(t, engine, "first", access.RoleUser)
	second := insertPluginTestUser(t, engine, "second", access.RoleUser)

	preview, err := service.PreviewBulkTelegramSettings(super, []int{second.ID, first.ID}, TelegramUserSettings{Disabled: true}, false)
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
	if err != nil || firstStatus.Version != 1 || !firstStatus.Settings.Disabled {
		t.Fatalf("first setting = %+v, err=%v", firstStatus, err)
	}

	conflicting, err := service.PreviewBulkTelegramSettings(super, []int{first.ID}, TelegramUserSettings{Disabled: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateOwnTelegramPreferences(first.ID, TelegramUserSettings{Disabled: true}, false, firstStatus.Version); err != nil {
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
	expiring, err := service.PreviewBulkTelegramSettings(super, []int{second.ID}, TelegramUserSettings{Disabled: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return base.Add(6 * time.Minute) }
	if err = service.ExecuteBulkTelegramSettings(super.ID, expiring.Ticket); !errors.Is(err, ErrPluginBulkTicketInvalid) {
		t.Fatalf("expired ticket error = %v", err)
	}
}

func TestEveryRoleKeepsIndependentOwnTelegramBinding(t *testing.T) {
	service, engine := newPluginConfigService(t)
	var users []models.User
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		user := insertPluginTestUser(t, engine, "telegram-own-"+string(role), role)
		users = append(users, user)
		status, err := service.GetOwnTelegramSettings(user.ID)
		if err != nil || status.Settings.ChatID != "" || status.Version != 0 {
			t.Fatalf("角色 %s 不应自动继承系统接收人：%v", role, err)
		}
		if _, err := service.UpdateOwnTelegramPreferences(user.ID, TelegramUserSettings{}, false, 0); !errors.Is(err, ErrTelegramBinding) {
			t.Fatal("未完成绑定时不应直接启用通知")
		}
		identity := TelegramUserSettings{ChatID: strconv.Itoa(10000 + user.ID), BotID: "123456", Verified: true, DisableLinkPreview: true}
		if _, err := service.UpdateOwnTelegramSettings(user.ID, identity, 0); !errors.Is(err, ErrTelegramBinding) {
			t.Fatal("任何角色都不能通过偏好设置伪造已授权身份")
		}
		if _, err := service.updateTelegramSettings(user.ID, identity, 0); err != nil {
			t.Fatal(err)
		}
		status, err = service.UpdateOwnTelegramPreferences(user.ID, TelegramUserSettings{Disabled: true}, false, 1)
		if err != nil || status.Settings.ChatID != identity.ChatID || !status.Settings.Verified || !status.Settings.Disabled || status.Version != 2 {
			t.Fatalf("角色 %s 停用通知时应保留本人绑定：%v", role, err)
		}
	}
	if _, err := service.UpdateOwnTelegramPreferences(users[0].ID, TelegramUserSettings{}, true, 2); err != nil {
		t.Fatal(err)
	}
	for index, user := range users {
		status, err := service.GetOwnTelegramSettings(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if status.Settings.ChatID != "" || status.Settings.Verified || status.Version != 3 {
				t.Fatal("超级管理员应能单独解绑本人身份")
			}
		} else if status.Settings.ChatID != strconv.Itoa(10000+user.ID) || status.Version != 2 {
			t.Fatal("超级管理员解绑不得改动其他账户的身份")
		}
	}
}

func TestTelegramBindingTracksBotIdentityNotRuntimeAvailability(t *testing.T) {
	service, _ := newPluginConfigService(t)
	settings := TelegramUserSettings{ChatID: "456789", BotID: "123456", Verified: true}
	for index, botID := range []string{"123456", "123456", "987654"} {
		_, err := service.ChangeSecret(telegramPluginID, "botToken", config.SecretChange{
			Operation: config.SecretReplace,
			Value:     &config.SecretInput{Plaintext: botID + ":" + strings.Repeat(string(rune('a'+index)), 32)},
		}, uint64(index))
		if err != nil {
			t.Fatal(err)
		}
		if got := service.TelegramBindingNeedsRebind(settings); got != (index == 2) {
			t.Fatal("同一机器人轮换令牌或临时停用不应强制重绑，更换机器人必须重绑")
		}
	}
	settings.Verified = false
	if !service.TelegramBindingNeedsRebind(settings) {
		t.Fatal("旧版手填身份必须重新授权")
	}
	if service.TelegramBindingNeedsRebind(TelegramUserSettings{}) {
		t.Fatal("尚未绑定不应显示为需要重新绑定")
	}
}

func TestTelegramBindingCancellationRemainsAccountAndSessionScoped(t *testing.T) {
	service, engine := newPluginConfigService(t)
	super := insertPluginTestUser(t, engine, "telegram-cancel-super", access.RoleSuperAdmin)
	user := insertPluginTestUser(t, engine, "telegram-cancel-user", access.RoleUser)
	actor := NotificationBindingActor{UserID: super.ID, SessionHash: strings.Repeat("a", 64),
		SessionVersion: super.SessionVersion, SecurityVersion: super.SecurityVersion}
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	service.telegramBindings = map[string]telegramBinding{"ticket": {Actor: actor,
		Status: TelegramBindingStatus{Phase: "waiting", ExpiresAt: now.Add(time.Minute)}}}
	if _, err := service.GetTelegramBinding(actor, "ticket"); err != nil {
		t.Fatal(err)
	}
	other := actor
	other.SessionHash = strings.Repeat("b", 64)
	if err := service.CancelTelegramBinding(other, "ticket"); !errors.Is(err, ErrTelegramBinding) {
		t.Fatal("同一账户的其他会话不能取消绑定")
	}
	other = NotificationBindingActor{UserID: user.ID, SessionHash: strings.Repeat("c", 64),
		SessionVersion: user.SessionVersion, SecurityVersion: user.SecurityVersion}
	if _, err := service.GetTelegramBinding(other, "ticket"); !errors.Is(err, ErrTelegramBinding) {
		t.Fatal("其他账户不能读取超级管理员的绑定请求")
	}
	if err := service.CancelTelegramBinding(other, "ticket"); !errors.Is(err, ErrTelegramBinding) {
		t.Fatal("其他账户不能取消超级管理员的绑定请求")
	}
	now = now.Add(2 * time.Minute)
	if _, err := service.GetTelegramBinding(actor, "ticket"); !errors.Is(err, ErrTelegramBinding) {
		t.Fatal("过期请求不能继续确认")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := service.CancelTelegramBinding(actor, "ticket"); err != nil {
			t.Fatal("本人的过期请求应能幂等取消", err)
		}
	}
	service.telegramBindings["revoked"] = telegramBinding{Actor: actor,
		Status: TelegramBindingStatus{Phase: "waiting", ExpiresAt: now.Add(time.Minute)}}
	if _, err := engine.ID(super.ID).Cols("security_version").Update(&models.User{SecurityVersion: super.SecurityVersion + 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetTelegramBinding(actor, "revoked"); !errors.Is(err, ErrTelegramBinding) {
		t.Fatal("安全版本变化必须撤销旧会话的绑定权限")
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
