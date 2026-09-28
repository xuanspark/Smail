package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigCredentialsAreEncryptedAtRest(t *testing.T) {
	oldRoot := ROOT_PATH
	root := t.TempDir()
	ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })

	original := &Config{
		WeChatPushSecret: "wechat-secret-value",
		TgBotToken:       "telegram-secret-value",
		WebPushToken:     "web-push-secret-value",
	}
	if err := WriteConfig(original); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{original.WeChatPushSecret, original.TgBotToken, original.WebPushToken} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("plaintext credential was written to configuration")
		}
	}
	loaded, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WeChatPushSecret != original.WeChatPushSecret || loaded.TgBotToken != original.TgBotToken || loaded.WebPushToken != original.WebPushToken {
		t.Fatal("configuration credentials did not round trip")
	}
	if original.WeChatPushSecret != "wechat-secret-value" {
		t.Fatal("write mutated the caller configuration")
	}
}

func TestNotificationSystemConfigurationRequiresSuperAdmin(t *testing.T) {
	for _, id := range []string{"telegram_push", "wechat_push"} {
		t.Run(id, func(t *testing.T) {
			original := PluginInstance{Enabled: true, Settings: json.RawMessage(`{"recipient":"original","enabled":true}`)}
			current := RuntimeConfig{Plugins: PluginsSectionConfig{Plugins: map[string]PluginInstance{id: original}}}
			for _, tc := range []struct {
				name   string
				modify func(*PluginInstance)
			}{
				{"enabled", func(p *PluginInstance) { p.Enabled = false }},
				{"settings", func(p *PluginInstance) { p.Settings = json.RawMessage(`{"recipient":"changed"}`) }},
				{"secret", func(p *PluginInstance) { p.Secret.Ciphertext = "changed" }},
				{"executable", func(p *PluginInstance) { p.Executable = "changed" }},
				{"capabilities", func(p *PluginInstance) { p.Permissions.Capabilities = []string{"network"} }},
				{"network", func(p *PluginInstance) {
					p.Permissions.Network = []PluginNetworkTarget{{Scheme: "https", Host: "example.com", Port: 443}}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					next := original
					tc.modify(&next)
					candidate := RuntimeConfig{Plugins: PluginsSectionConfig{Plugins: map[string]PluginInstance{id: next}}}
					for _, role := range []string{"super_admin", "admin", "user"} {
						err := AuthorizePluginChanges(AuditActor{UserID: "1", Role: role}, current, candidate)
						if (err == nil) != (role == "super_admin") {
							t.Fatalf("角色=%s，修改=%s，授权结果=%v", role, tc.name, err)
						}
					}
				})
			}
			for _, role := range []string{"super_admin", "admin", "user"} {
				actor := AuditActor{UserID: "1", Role: role}
				if err := AuthorizePluginChanges(actor, RuntimeConfig{}, current); (err == nil) != (role == "super_admin") {
					t.Fatalf("角色=%s，首次配置授权结果=%v", role, err)
				}
				if err := AuthorizePluginChanges(actor, current, RuntimeConfig{}); (err == nil) != (role == "super_admin") {
					t.Fatalf("角色=%s，移除配置授权结果=%v", role, err)
				}
			}
		})
	}
}

func TestAdminCanCarryUnchangedNotificationConfiguration(t *testing.T) {
	for _, id := range []string{"telegram_push", "wechat_push"} {
		current := RuntimeConfig{Plugins: PluginsSectionConfig{Plugins: map[string]PluginInstance{
			id:           {Enabled: true, Settings: json.RawMessage(`{"recipient":"original","enabled":true}`)},
			"spam_block": {Settings: json.RawMessage(`{"score":1}`)},
		}}}
		candidate := RuntimeConfig{Plugins: PluginsSectionConfig{Plugins: map[string]PluginInstance{
			id:           {Enabled: true, Settings: json.RawMessage(`{ "enabled": true, "recipient": "original" }`)},
			"spam_block": {Settings: json.RawMessage(`{"score":2}`)},
		}}}
		if err := AuthorizePluginChanges(AuditActor{UserID: "2", Role: "admin"}, current, candidate); err != nil {
			t.Fatalf("修改垃圾邮件参数不应误拦截未变化的渠道 %s：%v", id, err)
		}
		if err := AuthorizePluginChanges(AuditActor{UserID: "3", Role: "user"}, current, candidate); err == nil {
			t.Fatal("普通用户不应修改系统插件配置")
		}
		invalid := candidate.Plugins.Plugins[id]
		invalid.Settings = append(invalid.Settings, []byte(" {}")...)
		candidate.Plugins.Plugins[id] = invalid
		if err := AuthorizePluginChanges(AuditActor{UserID: "2", Role: "admin"}, current, candidate); err == nil {
			t.Fatal("尾随 JSON 不应视为未变化的系统配置")
		}
	}
}

func TestConfigPrefixCredentialRoundTrip(t *testing.T) {
	oldRoot := ROOT_PATH
	root := t.TempDir()
	ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })

	original := &Config{WeChatPushSecret: "enc:v1:provider-secret"}
	if err := WriteConfig(original); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WeChatPushSecret != original.WeChatPushSecret {
		t.Fatal("prefix credential did not round trip through configuration")
	}
}

func TestReadConfigRejectsLegacyPlaintextCredentials(t *testing.T) {
	oldRoot := ROOT_PATH
	root := t.TempDir()
	ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })

	legacy := &Config{
		WeChatPushSecret: "legacy-wechat-secret",
		TgBotToken:       "legacy-telegram-token",
		WebPushToken:     "legacy-web-push-token",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeConfigFile("config.json", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfig(); err == nil {
		t.Fatal("legacy plaintext credential was accepted")
	}
	stored, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{legacy.WeChatPushSecret, legacy.TgBotToken, legacy.WebPushToken} {
		if !bytes.Contains(stored, []byte(plaintext)) {
			t.Fatal("rejected configuration was modified")
		}
	}
}

func TestConfigRejectsTamperedCredential(t *testing.T) {
	oldRoot := ROOT_PATH
	root := t.TempDir()
	ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })

	if err := WriteConfig(&Config{WeChatPushSecret: "wechat-secret-value"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("enc:v1:")
	index := bytes.Index(raw, marker)
	if index < 0 {
		t.Fatal("encrypted credential marker was not found")
	}
	change := index + len(marker)
	if raw[change] == 'A' {
		raw[change] = 'B'
	} else {
		raw[change] = 'A'
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfig(); err == nil {
		t.Fatal("tampered credential was accepted")
	}
}
