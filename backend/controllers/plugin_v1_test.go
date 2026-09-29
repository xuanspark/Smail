package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/credential"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

type telegramControllerTestHook struct {
	pluginTestHook
	botInfo        framework.BotInfo
	botInfoErr     error
	testPayload    json.RawMessage
	testMessageErr error
	userID         int
	userSettings   json.RawMessage
}

func (h *telegramControllerTestHook) ConfigureUserSettings(userID int, settings json.RawMessage) error {
	h.userID = userID
	h.userSettings = append(h.userSettings[:0], settings...)
	return nil
}

func (h *telegramControllerTestHook) GetBotInfo() (framework.BotInfo, error) {
	return h.botInfo, h.botInfoErr
}

func (h *telegramControllerTestHook) TestMessage(settings json.RawMessage) error {
	h.testPayload = append(h.testPayload[:0], settings...)
	return h.testMessageErr
}

func TestTelegramUserSettingsRequiresLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plugin/settings/telegram_push/user", nil)
	recorder := httptest.NewRecorder()
	GetTelegramUserSettings(nil, recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "无权访问或请求不满足安全条件") {
		t.Fatalf("unauthenticated GET status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/plugin/settings/telegram_push/user", strings.NewReader(`{}`))
	recorder = httptest.NewRecorder()
	UpdateTelegramUserSettings(&appcontext.Context{}, recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "无权访问或请求不满足安全条件") {
		t.Fatalf("unauthenticated PUT status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTelegramUserSettingsRejectsUnknownAndTrailingFields(t *testing.T) {
	service, engine := newTelegramControllerConfigService(t)
	previousService := hooks.PluginConfigService()
	hooks.SetPluginConfigService(service)
	t.Cleanup(func() { hooks.SetPluginConfigService(previousService) })
	user := models.User{Account: "telegram-parse-user", Name: "telegram-parse-user", Password: "hash"}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		`{"enabled":false,"chatId":"123","version":0}`,
		`{"enabled":false,"version":0} {}`,
	}
	for _, body := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/plugin/settings/telegram_push/user", bytes.NewBufferString(body))
		UpdateTelegramUserSettings(&appcontext.Context{UserID: user.ID, RecentAuth: true}, recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "请求参数格式不正确") {
			t.Fatalf("body=%s status=%d response=%s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestTelegramUserSettingsUsesSessionUserAndVersion(t *testing.T) {
	service, engine := newTelegramControllerConfigService(t)
	previousService := hooks.PluginConfigService()
	hooks.SetPluginConfigService(service)
	t.Cleanup(func() { hooks.SetPluginConfigService(previousService) })

	user := models.User{Account: "controller-user", Name: "controller-user", Password: "hash"}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPut, "/api/v1/plugin/settings/telegram_push/user", strings.NewReader(`{"enabled":false,"showContent":true,"version":0}`))
	UpdateTelegramUserSettings(&appcontext.Context{UserID: user.ID, RecentAuth: true}, firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK || !strings.Contains(firstRecorder.Body.String(), `"version":1`) {
		t.Fatalf("first update status=%d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	if strings.Contains(firstRecorder.Body.String(), "bot-token") {
		t.Fatal("secret material appeared in settings response")
	}

	staleRecorder := httptest.NewRecorder()
	staleRequest := httptest.NewRequest(http.MethodPut, "/api/v1/plugin/settings/telegram_push/user", strings.NewReader(`{"enabled":false,"showContent":false,"version":0}`))
	UpdateTelegramUserSettings(&appcontext.Context{UserID: user.ID, RecentAuth: true}, staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusOK || !strings.Contains(staleRecorder.Body.String(), "配置已被其他操作更新") {
		t.Fatalf("stale update status=%d body=%s", staleRecorder.Code, staleRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	GetTelegramUserSettings(&appcontext.Context{UserID: user.ID}, getRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/plugin/settings/telegram_push/user", nil))
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"bound":false`) || strings.Contains(getRecorder.Body.String(), "-1001234567890") {
		t.Fatalf("GET status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
}

func TestTelegramBotInfoDoesNotExposeSecret(t *testing.T) {
	previousHooks := hooks.HookList
	hook := &telegramControllerTestHook{botInfo: framework.BotInfo{Username: "smail_test_bot", BotLink: "https://t.me/smail_test_bot"}}
	hooks.HookList = map[string]framework.EmailHook{"telegram_push": hook}
	t.Cleanup(func() { hooks.HookList = previousHooks })

	recorder := httptest.NewRecorder()
	GetTelegramBotInfo(&appcontext.Context{UserID: 7}, recorder, httptest.NewRequest(http.MethodGet, "/api/v1/plugin/settings/telegram_push/bot", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "smail_test_bot") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "bot-token") || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("bot response exposed secret material: %s", recorder.Body.String())
	}
}

func TestTelegramTestMessageRequiresCurrentAccountBinding(t *testing.T) {
	service, engine := newTelegramControllerConfigService(t)
	previousService := hooks.PluginConfigService()
	hooks.SetPluginConfigService(service)
	t.Cleanup(func() { hooks.SetPluginConfigService(previousService) })

	user := models.User{Account: "telegram-test-user", Name: "telegram-test-user", Password: "hash"}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}

	previousHooks := hooks.HookList
	hook := &telegramControllerTestHook{}
	hooks.HookList = map[string]framework.EmailHook{"telegram_push": hook}
	t.Cleanup(func() { hooks.HookList = previousHooks })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plugin/settings/telegram_push/test", strings.NewReader(`{"settings":{"chatId":"123"}}`))
	TestTelegramMessage(&appcontext.Context{UserID: user.ID, RecentAuth: true}, recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "请先绑定本人 Telegram 并启用通知") {
		t.Fatalf("unbound test message status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if hook.testPayload != nil {
		t.Fatal("caller-supplied Chat ID reached plugin without a verified binding")
	}
}

func newTelegramControllerConfigService(t *testing.T) (*pluginservice.ConfigService, *xorm.Engine) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "telegram-controller.db"))
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
	service, err := pluginservice.NewConfigService(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	return service, engine
}
