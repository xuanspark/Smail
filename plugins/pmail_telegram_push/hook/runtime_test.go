package hook

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func validRuntimePayload() []byte {
	payload := pluginRuntimeConfig{
		PluginID:     "telegram_push",
		Generation:   1,
		Settings:     json.RawMessage(`{"timeout":30,"webDomain":"mail.example.test","httpsEnabled":true}`),
		Secrets:      map[string]string{"botToken": "secret-token"},
		Capabilities: []string{"network"},
		Network:      []pluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}

func TestParseRuntimeConfigAcceptsAuthorizedPayload(t *testing.T) {
	runtime, settings, token, err := parseRuntimeConfig(validRuntimePayload())
	if err != nil {
		t.Fatalf("parseRuntimeConfig() error = %v", err)
	}
	if runtime.PluginID != pluginID || runtime.Generation != 1 || settings.Timeout != defaultTimeout || token != "secret-token" {
		t.Fatalf("unexpected parsed configuration: id=%q timeout=%d token=%q", runtime.PluginID, settings.Timeout, token)
	}
}

func TestParseRuntimeConfigRejectsZeroGeneration(t *testing.T) {
	var payload pluginRuntimeConfig
	if err := json.Unmarshal(validRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Generation = 0
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = parseRuntimeConfig(data); err == nil {
		t.Fatal("zero plugin configuration generation was accepted")
	}
}

func TestParseRuntimeConfigAcceptsHostManagedLocalProxy(t *testing.T) {
	var payload pluginRuntimeConfig
	if err := json.Unmarshal(validRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Settings = json.RawMessage(`{"proxy":"http://127.0.0.1:9000","timeout":30}`)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, settings, _, err := parseRuntimeConfig(data)
	if err != nil || settings.Proxy != "http://127.0.0.1:9000" {
		t.Fatal("host-managed local proxy was rejected")
	}
}

func TestParseRuntimeConfigRejectsSecurityBoundaryViolations(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*pluginRuntimeConfig)
		wantError string
	}{
		{name: "plugin id mismatch", mutate: func(payload *pluginRuntimeConfig) { payload.PluginID = "other" }, wantError: "\u63d2\u4ef6 ID \u4e0d\u5339\u914d"},
		{name: "missing token", mutate: func(payload *pluginRuntimeConfig) { payload.Secrets = nil }, wantError: "Telegram Bot Token \u672a\u914d\u7f6e"},
		{name: "unknown secret", mutate: func(payload *pluginRuntimeConfig) { payload.Secrets["other"] = "value" }, wantError: "Telegram 插件密钥字段无效"},
		{name: "missing capability", mutate: func(payload *pluginRuntimeConfig) { payload.Capabilities = nil }, wantError: "Telegram 插件能力权限无效"},
		{name: "extra capability", mutate: func(payload *pluginRuntimeConfig) { payload.Capabilities = []string{"network", "filesystem"} }, wantError: "Telegram 插件能力权限无效"},
		{name: "missing network permission", mutate: func(payload *pluginRuntimeConfig) { payload.Network = nil }, wantError: "Telegram \u7f51\u7edc\u6743\u9650\u672a\u914d\u7f6e"},
		{name: "wrong network host", mutate: func(payload *pluginRuntimeConfig) {
			payload.Network = []pluginNetworkTarget{{Scheme: "https", Host: "example.test", Port: 443}}
		}, wantError: "Telegram \u7f51\u7edc\u6743\u9650\u672a\u914d\u7f6e"},
		{name: "wrong network port", mutate: func(payload *pluginRuntimeConfig) {
			payload.Network = []pluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 8443}}
		}, wantError: "Telegram \u7f51\u7edc\u6743\u9650\u672a\u914d\u7f6e"},
		{name: "extra network target", mutate: func(payload *pluginRuntimeConfig) {
			payload.Network = append(payload.Network, pluginNetworkTarget{Scheme: "https", Host: "example.test", Port: 443})
		}, wantError: "Telegram \u7f51\u7edc\u6743\u9650\u672a\u914d\u7f6e"},
		{name: "proxy bypass", mutate: func(payload *pluginRuntimeConfig) {
			payload.Settings = json.RawMessage(`{"proxy":"http://192.168.1.1:8080"}`)
		}, wantError: "Telegram 代理仅支持带端口的本机 HTTP 地址"},
		{name: "timeout too large", mutate: func(payload *pluginRuntimeConfig) { payload.Settings = json.RawMessage(`{"timeout":121}`) }, wantError: "Telegram \u8bf7\u6c42\u8d85\u65f6\u65f6\u95f4\u65e0\u6548"},
		{name: "unknown setting", mutate: func(payload *pluginRuntimeConfig) { payload.Settings = json.RawMessage(`{"unknown":true}`) }, wantError: "Telegram \u63d2\u4ef6\u914d\u7f6e\u65e0\u6548"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload pluginRuntimeConfig
			if err := json.Unmarshal(validRuntimePayload(), &payload); err != nil {
				t.Fatal(err)
			}
			test.mutate(&payload)
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = parseRuntimeConfig(data)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("error exposed secret material: %v", err)
			}
		})
	}
}

func TestParseRuntimeConfigRejectsTrailingJSON(t *testing.T) {
	data := append(validRuntimePayload(), []byte(` {}`)...)
	_, _, _, err := parseRuntimeConfig(data)
	if err == nil || !strings.Contains(err.Error(), "\u63d2\u4ef6\u8fd0\u884c\u65f6\u914d\u7f6e\u65e0\u6548") {
		t.Fatalf("error = %v, want trailing JSON rejection", err)
	}
}

func TestSettingStoreKeepsSettingsScopedByUser(t *testing.T) {
	store := newSettingStore()
	if err := store.update(10, telegramSetting{ChatID: "-1001234567890", ShowContent: true}); err != nil {
		t.Fatal(err)
	}
	if got := store.get(10); got.ChatID != "-1001234567890" {
		t.Fatalf("user setting was not saved: %+v", got)
	}
	if got := store.get(11); got.ChatID != "" {
		t.Fatalf("user setting crossed user boundary: %+v", got)
	}
}

func TestSettingStoreRejectsInvalidChatID(t *testing.T) {
	store := newSettingStore()
	for _, chatID := range []string{"0", "abc", "--1", "+123"} {
		if err := store.update(1, telegramSetting{ChatID: chatID}); err == nil {
			t.Fatalf("chat ID %q was accepted", chatID)
		}
	}
}

func TestSettingStoreConcurrentUpdates(t *testing.T) {
	store := newSettingStore()
	var wg sync.WaitGroup
	for userID := 1; userID <= 32; userID++ {
		userID := userID
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if err := store.update(userID, telegramSetting{ChatID: "-1001234567890"}); err != nil {
					t.Errorf("update() error = %v", err)
				}
				_ = store.get(userID)
			}
		}()
	}
	wg.Wait()
}
