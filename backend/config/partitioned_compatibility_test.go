package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPartitionedCompatibilityStartupAndWriteProtection(t *testing.T) {
	root := t.TempDir()
	oldRoot, oldArgs, oldDevelopment := ROOT_PATH, os.Args, DevelopmentMode
	oldInstance, oldSnapshot := Instance, CurrentConfig()
	ROOT_PATH, os.Args = root, []string{"smail"}
	t.Cleanup(func() {
		ROOT_PATH, os.Args, DevelopmentMode = oldRoot, oldArgs, oldDevelopment
		Instance = oldInstance
		PublishConfigSnapshot(oldSnapshot)
	})
	writeLegacyMigrationFixture(t, root, map[string]any{
		"logLevel": "info", "domain": "mail.example.com", "webDomain": "mail.example.com",
		"dbType": "sqlite", "dbDSN": "./config/existing.db", "isInit": true,
		"sslType": "1", "SSLPublicKeyPath": "./config/ssl/site.crt", "SSLPrivateKeyPath": "./config/ssl/site.key",
		"httpPort": 80, "httpsPort": 443, "httpsEnabled": 1,
	})
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Init(); err != nil {
			t.Fatal(err)
		}
		cfg := CurrentConfig()
		if !cfg.IsSetupReady() || cfg.DbType != DBTypeSQLite || cfg.DbDSN != filepath.Join(root, "config", "existing.db") {
			t.Fatal("partitioned startup lost initialization or database identity")
		}
		if cfg.WebDomain != "mail.example.com" || cfg.SSLPublicKeyPath != filepath.Join(root, "config", "ssl", "site.crt") {
			t.Fatal("partitioned startup lost domain or certificate path")
		}
	}
	cfg, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(cfg); err == nil {
		t.Fatal("legacy writer must not overwrite partitioned config")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading or legacy writing changed partitioned root")
	}
	if err := os.WriteFile(filepath.Join(root, "config", "smtp.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Init(); err == nil {
		t.Fatal("corrupted partition must not silently fall back to legacy startup")
	}
}
