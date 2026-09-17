package hooks

import (
	"context"
	"encoding/json"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	"git.pcux.cn/xuans/Smail/utils/credential"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"xorm.io/xorm"
)

func TestPluginAllowListRejectsUnsafeNames(t *testing.T) {
	allowed := pluginAllowList([]string{"safe-plugin", "safe-plugin", "../outside", "nested/file", "*", ""})
	if len(allowed) != 1 || !allowed["safe-plugin"] {
		t.Fatalf("unexpected allow list: %#v", allowed)
	}
}

func TestIsPluginExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !isPluginExecutable("safe.exe", 0600) || isPluginExecutable("safe", 0600) {
			t.Fatal("Windows executable validation is incorrect")
		}
		return
	}
	if !isPluginExecutable("safe", os.FileMode(0700)) || isPluginExecutable("safe.txt", os.FileMode(0700)) || isPluginExecutable("safe", os.FileMode(0600)) {
		t.Fatal("Unix executable validation is incorrect")
	}
}

func TestSecurePluginFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	if !securePluginFile(0700) || securePluginFile(0775) || securePluginFile(0702) || securePluginFile(os.ModeSetuid|0700) || securePluginFile(os.ModeSetgid|0700) {
		t.Fatal("plugin file permission validation is incorrect")
	}
}

func TestMinimalPluginEnvUsesAllowList(t *testing.T) {
	t.Setenv("SMAIL_PLUGIN_SECRET", "should-not-be-forwarded")
	t.Setenv("DB_PASSWORD", "should-not-be-forwarded")
	env := minimalPluginEnv("C:/smail")
	values := make(map[string]string, len(env))
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	if values["SMAIL_ROOT"] != "C:/smail" || values["SMAIL_PLUGIN"] != "1" {
		t.Fatalf("plugin identity environment is incorrect: %#v", values)
	}
	for _, name := range []string{"SMAIL_PLUGIN_SECRET", "DB_PASSWORD", "HOME"} {
		if _, ok := values[name]; ok {
			t.Fatalf("sensitive environment variable was forwarded: %s", name)
		}
	}
}

func TestProductionPluginIsolationPolicy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows production policy test")
	}
	if _, err := newPluginLaunchSpec("plugin.exe", ".", "plugin.exe", "1.socket", nil); err == nil {
		t.Fatal("Windows production plugin isolation must fail closed")
	}
}

func TestPersistPluginConfigReplacesFileAtomically(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(workingDirectory)
		ResetHooks()
	})
	if err = os.Mkdir("plugins", 0700); err != nil {
		t.Fatal(err)
	}
	ResetHooks()
	if !RegisterHook("spam_block", &HookSender{name: "spam_block.exe"}) {
		t.Fatal("plugin hook registration failed")
	}
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		// 普通用户创建的插件目录不满足生产所有者要求，必须拒绝写入。
		if err := PersistPluginConfig("spam_block", `{"url":"https://model.example.com"}`); err == nil {
			t.Fatal("non-root-owned plugin directory was accepted")
		}
		return
	}
	for _, payload := range []string{`{"url":"https://model.example.com"}`, `{"url":"https://model.example.com/v2"}`} {
		if err = PersistPluginConfig("spam_block", payload); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "plugins", "spam_block_config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"url":"https://model.example.com/v2"}` {
		t.Fatalf("plugin configuration content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("plugin configuration permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRegisterHookRejectsUnsafeNames(t *testing.T) {
	ResetHooks()
	t.Cleanup(ResetHooks)
	for _, name := range []string{"../unsafe", "nested/name", `nested\name`, "name with space", strings.Repeat("a", 65)} {
		if RegisterHook(name, &HookSender{}) {
			t.Fatalf("unsafe hook name was accepted: %q", name)
		}
	}
	if !RegisterHook("safe-plugin_1", &HookSender{}) {
		t.Fatal("safe hook name was rejected")
	}
}

func TestPluginDefinitionsFromRuntimeUsesEnabledLogicalIDs(t *testing.T) {
	runtimeConfig := config.RuntimeConfig{
		Plugins: config.PluginsSectionConfig{Plugins: map[string]config.PluginInstance{
			"spam_block":    {Enabled: true},
			"telegram_push": {Enabled: false},
		}},
	}
	definitions, err := pluginDefinitionsFromRuntime(runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].LogicalID != "spam_block" {
		t.Fatalf("definitions = %+v", definitions)
	}
}

func TestPluginRuntimeConfigRejectsStaleOrEscalatedBinding(t *testing.T) {
	base := config.PluginInstance{
		Enabled: true,
		Permissions: config.PluginPermissions{
			Capabilities: []string{"network"},
			Network:      []config.PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
		},
	}
	tests := []struct {
		name   string
		mutate func(*config.PluginInstance)
	}{
		{name: "disabled", mutate: func(instance *config.PluginInstance) { instance.Enabled = false }},
		{name: "extra capability", mutate: func(instance *config.PluginInstance) {
			instance.Permissions.Capabilities = append(instance.Permissions.Capabilities, "filesystem")
		}},
		{name: "legacy secret", mutate: func(instance *config.PluginInstance) {
			instance.Secret.Ciphertext = "encrypted"
			instance.Secret.Version = 1
			instance.Secret.UpdatedAt = time.Now()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance := base
			test.mutate(&instance)
			if _, err := pluginRuntimeConfig("telegram_push", 1, instance); err == nil {
				t.Fatal("unsafe plugin binding was accepted")
			}
		})
	}
}

func TestPluginRuntimeConfigBuildsOnlyCatalogCapabilities(t *testing.T) {
	instance := config.PluginInstance{
		Enabled:  true,
		Settings: json.RawMessage(`{"timeout":10}`),
		Permissions: config.PluginPermissions{
			Capabilities: []string{"network"},
			Network:      []config.PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
		},
	}
	payload, err := pluginRuntimeConfig("telegram_push", 7, instance)
	if err != nil {
		t.Fatal(err)
	}
	if payload.PluginID != "telegram_push" || payload.Generation != 7 || len(payload.Capabilities) != 1 || payload.Capabilities[0] != "network" {
		t.Fatalf("unexpected capabilities: %+v", payload.Capabilities)
	}
	if len(payload.Network) != 1 || payload.Network[0] != (framework.PluginNetworkTarget{Scheme: "https", Host: "api.telegram.org", Port: 443}) {
		t.Fatalf("unexpected network targets: %+v", payload.Network)
	}
	if string(payload.Settings) != `{"timeout":10}` {
		t.Fatalf("unexpected settings: %s", payload.Settings)
	}
}

func TestPluginRuntimeConfigRejectsMissingGeneration(t *testing.T) {
	instance := config.PluginInstance{
		Enabled: true,
		Permissions: config.PluginPermissions{
			Capabilities: []string{"network"},
		},
	}
	if _, err := pluginRuntimeConfig("spam_block", 0, instance); err == nil {
		t.Fatal("zero plugin configuration generation was accepted")
	}
}

func TestPluginRuntimeConfigIsolatesPluginSecrets(t *testing.T) {
	service, engine := newHookPluginConfigService(t)
	_ = engine
	previous := pluginConfigService
	SetPluginConfigService(service)
	t.Cleanup(func() { SetPluginConfigService(previous) })

	for _, entry := range []struct {
		pluginID string
		field    string
		value    string
	}{
		{pluginID: "wechat_push", field: "appSecret", value: "wechat-secret-for-test"},
		{pluginID: "telegram_push", field: "botToken", value: "telegram-secret-for-test"},
	} {
		if _, err := service.ChangeSecret(entry.pluginID, entry.field, config.SecretChange{
			Operation: config.SecretReplace,
			Value:     &config.SecretInput{Plaintext: entry.value},
		}, 0); err != nil {
			t.Fatal(err)
		}
	}

	wechat, err := pluginRuntimeConfig("wechat_push", 3, config.PluginInstance{
		Enabled:  true,
		Settings: json.RawMessage(`{"appId":"app-id","templateId":"template-id","userId":"user-id","webDomain":"mail.example.com"}`),
		Permissions: config.PluginPermissions{
			Capabilities: []string{"network"},
			Network:      []config.PluginNetworkTarget{{Scheme: "https", Host: "api.weixin.qq.com", Port: 443}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(wechat.Secrets) != 1 || wechat.Secrets["appSecret"] != "wechat-secret-for-test" {
		t.Fatalf("unexpected WeChat secrets: %#v", wechat.Secrets)
	}
	if _, exists := wechat.Secrets["botToken"]; exists {
		t.Fatal("Telegram secret was included in WeChat runtime configuration")
	}
	wechatData, err := json.Marshal(wechat)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wechatData), "telegram-secret-for-test") {
		t.Fatal("Telegram secret appeared in WeChat runtime configuration")
	}

	telegram, err := pluginRuntimeConfig("telegram_push", 4, config.PluginInstance{
		Enabled:  true,
		Settings: json.RawMessage(`{"timeout":10}`),
		Permissions: config.PluginPermissions{
			Capabilities: []string{"network"},
			Network:      []config.PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(telegram.Secrets) != 1 || telegram.Secrets["botToken"] != "telegram-secret-for-test" {
		t.Fatalf("unexpected Telegram secrets: %#v", telegram.Secrets)
	}
	if _, exists := telegram.Secrets["appSecret"]; exists {
		t.Fatal("WeChat secret was included in Telegram runtime configuration")
	}
	telegramData, err := json.Marshal(telegram)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(telegramData), "wechat-secret-for-test") {
		t.Fatal("WeChat secret appeared in Telegram runtime configuration")
	}
}

func TestPluginCapabilitiesMatchCatalogStrictly(t *testing.T) {
	definition, ok := pluginservice.Lookup("telegram_push")
	if !ok {
		t.Fatal("telegram plugin definition is missing")
	}
	valid := framework.Capabilities{
		PluginID:     "telegram_push",
		Capabilities: []string{"network", "userSettings", "settingsHtml"},
		Settings:     []string{"proxy", "timeout", "debug", "webDomain", "httpsEnabled"},
		Protocol:     1,
	}
	if !pluginCapabilitiesMatch(definition, valid) {
		t.Fatal("valid plugin capability handshake was rejected")
	}
	valid.Capabilities[0] = "filesystem"
	if pluginCapabilitiesMatch(definition, valid) {
		t.Fatal("unexpected plugin capability was accepted")
	}
}

func newHookPluginConfigService(t *testing.T) (*pluginservice.ConfigService, *xorm.Engine) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "hook-plugin-config.db"))
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

func TestLegacyPluginEnvironmentDoesNotForwardWeChatSecret(t *testing.T) {
	previous := config.CurrentConfig()
	config.PublishConfigSnapshot(&config.Config{
		WeChatPushAppId:      "app-id",
		WeChatPushSecret:     "secret-must-not-be-forwarded",
		WeChatPushTemplateId: "template-id",
		WeChatPushUserId:     "user-id",
	})
	t.Cleanup(func() { config.PublishConfigSnapshot(previous) })
	env := pluginEnvironment("C:/smail", "wechat_push")
	for _, item := range env {
		if strings.HasPrefix(item, "SMAIL_WECHAT_SECRET=") {
			t.Fatal("legacy WeChat secret was forwarded to plugin environment")
		}
	}
}
