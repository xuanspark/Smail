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
