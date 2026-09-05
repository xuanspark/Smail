package res_init

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestStartConfigManagerUsesServiceRoot(t *testing.T) {
	root := t.TempDir()
	previousRoot, previousDevelopment := config.ROOT_PATH, config.DevelopmentMode
	config.ROOT_PATH, config.DevelopmentMode = root, false
	t.Cleanup(func() {
		stopConfigManager()
		config.ROOT_PATH, config.DevelopmentMode = previousRoot, previousDevelopment
	})
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"domain": "mail.example.com", "webDomain": "mail.example.com",
		"dbType": "sqlite", "dbDSN": "./config/existing.db", "isInit": true,
		"httpPort": 80, "httpsPort": 443, "httpsEnabled": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.MigrateLegacyConfig(root, config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	if err := startConfigManager(); err != nil {
		t.Fatal(err)
	}
	manager := config.CurrentManager()
	if manager == nil || manager.Snapshot().Generation != 1 {
		t.Fatal("migrated configuration manager is unavailable")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "config")); !os.IsNotExist(err) {
		t.Fatal("manager must not create a nested config directory")
	}
}
