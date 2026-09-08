package config

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
	"github.com/go-sql-driver/mysql"
)

func TestPartitionedDatabaseConnectionPreservesMigrationTimezone(t *testing.T) {
	vault, err := credential.NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "test-password"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []DatabaseType{DatabaseTypeMySQL, DatabaseTypePostgreSQL} {
		_, dsn, err := compatibilityDatabase(DatabaseConfig{Type: kind, Host: "::1", Port: 3306, Username: "test", Name: "test", Password: secret}, vault)
		if err != nil {
			t.Fatal("runtime connection failed")
		}
		if kind == DatabaseTypeMySQL {
			settings, err := mysql.ParseDSN(dsn)
			_, rawQuery, _ := strings.Cut(dsn, "?")
			query, queryErr := url.ParseQuery(rawQuery)
			// charset 会被驱动解析为内部选项，不再保留在 Params 中。
			if err != nil || queryErr != nil || !settings.ParseTime || settings.Params["time_zone"] != "'+00:00'" || query.Get("charset") != "utf8mb4" {
				t.Fatal("MySQL runtime timezone differs from migration")
			}
		} else {
			uri, err := url.Parse(dsn)
			if err != nil || uri.Query().Get("timezone") != "UTC" || uri.Query().Get("search_path") != "public" || uri.Query().Get("sslmode") != "require" {
				t.Fatal("PostgreSQL runtime options differ from migration")
			}
		}
	}
}

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
		if !cfg.IsSetupReady() || cfg.DbType != DBTypeSQLite || cfg.DbDSN != filepath.Join(root, "data", "existing.db") {
			t.Fatal("partitioned startup lost initialization or database identity")
		}
		if cfg.WebDomain != "mail.example.com" || cfg.SSLPublicKeyPath != filepath.Join(root, "data", "ssl", "site.crt") {
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
