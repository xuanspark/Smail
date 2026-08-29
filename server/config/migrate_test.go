package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

func writeLegacyMigrationFixture(t *testing.T, root string, value any) []byte {
	t.Helper()
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMigrateLegacyConfigPreservesBackupAndLoadsPartitions(t *testing.T) {
	root := t.TempDir()
	legacy := map[string]any{
		"logLevel":             "info",
		"domain":               "mail.example.com",
		"domains":              []string{"mail.example.com", "example.com"},
		"webDomain":            "mail.example.com",
		"sslType":              "1",
		"SSLPrivateKeyPath":    "./config/ssl/private.key",
		"SSLPublicKeyPath":     "./config/ssl/public.crt",
		"dbDSN":                "./config/smail.db",
		"dbType":               "sqlite",
		"httpsEnabled":         1,
		"httpPort":             80,
		"httpsPort":            443,
		"weChatPushAppId":      "app-id",
		"weChatPushSecret":     "wechat-secret",
		"weChatPushTemplateId": "template-id",
		"isInit":               true,
	}
	original := writeLegacyMigrationFixture(t, root, legacy)

	result, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Generation != 1 || result.BackupPath == "" {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("legacy backup does not match the original configuration")
	}
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	runtime, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Root.Database.LocalRef != "smail.db" || runtime.Plugins.Plugins["wechat_push"].Secret.Ciphertext == "" {
		t.Fatalf("unexpected migrated runtime: %+v", runtime)
	}
	for _, name := range []string{"config.json", "smtp.json", "email.json", "ssl.json", "plugins.json"} {
		data, readErr := os.ReadFile(filepath.Join(root, "config", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte("wechat-secret")) {
			t.Fatalf("plaintext credential leaked to %s", name)
		}
	}
}

func TestPlanLegacyMigrationReportsUnknownFieldsWithoutOverwritingConfig(t *testing.T) {
	root := t.TempDir()
	original := writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":       "smail.db",
		"dbType":      "sqlite",
		"unsupported": true,
	})
	plan, err := PlanLegacyMigration(root, RuntimeValidationOptions{})
	if !errors.Is(err, ErrMigrationUnknownField) {
		t.Fatalf("error = %v", err)
	}
	if len(plan.UnknownFields) != 1 || plan.UnknownFields[0] != "unsupported" {
		t.Fatalf("unexpected unknown fields: %+v", plan.UnknownFields)
	}
	current, readErr := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("migration planning overwrote the original configuration")
	}
}

func TestMigrateLegacyConfigRejectsManualCredentialsWithoutOverwritingConfig(t *testing.T) {
	root := t.TempDir()
	original := writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":           "smail.db",
		"dbType":          "sqlite",
		"domain":          "mail.example.com",
		"domains":         []string{"mail.example.com"},
		"webDomain":       "mail.example.com",
		"sslType":         "0",
		"isInit":          true,
		"httpsEnabled":    1,
		"httpsPort":       443,
		"tgBotToken":      "token",
		"tgChatId":        "123456",
		"httpPort":        80,
		"spamFilterLevel": 0,
	})
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); !errors.Is(err, ErrMigrationUnsafeValue) {
		t.Fatalf("error = %v", err)
	}
	current, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("failed migration overwrote the original configuration")
	}
	for _, name := range []string{"smtp.json", "email.json", "ssl.json", "plugins.json"} {
		if _, err := os.Stat(filepath.Join(root, "config", name)); !os.IsNotExist(err) {
			t.Fatalf("failed migration created %s", name)
		}
	}
}

func TestMigrateLegacyConfigReencryptsExistingLegacyCredential(t *testing.T) {
	root := t.TempDir()
	if _, err := credential.NewVault(root); err != nil {
		t.Fatal(err)
	}
	encrypted, err := credential.Encrypt(root, "telegram-token")
	if err != nil {
		t.Fatal(err)
	}
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":        "smail.db",
		"dbType":       "sqlite",
		"domain":       "mail.example.com",
		"domains":      []string{"mail.example.com"},
		"webDomain":    "mail.example.com",
		"sslType":      "0",
		"isInit":       true,
		"httpsEnabled": 1,
		"httpsPort":    443,
		"tgBotToken":   encrypted,
		"httpPort":     80,
	})
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenStoredSecret(mustMigrationVault(t, root), runtime.Plugins.Plugins["telegram_push"].Secret)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "telegram-token" {
		t.Fatalf("opened secret = %q", opened)
	}
}

func TestMigrateLegacyConfigResumesWithEquivalentExistingSecretSection(t *testing.T) {
	root := t.TempDir()
	legacy := map[string]any{
		"dbDSN":           "smail.db",
		"dbType":          "sqlite",
		"domain":          "mail.example.com",
		"domains":         []string{"mail.example.com"},
		"webDomain":       "mail.example.com",
		"sslType":         "0",
		"isInit":          true,
		"httpsEnabled":    1,
		"httpPort":        80,
		"httpsPort":       443,
		"tgBotToken":      "telegram-token",
		"pluginAllowList": []string{"telegram_push"},
	}
	original := writeLegacyMigrationFixture(t, root, legacy)
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	pluginsPath := filepath.Join(root, "config", "plugins.json")
	pluginsBefore, err := os.ReadFile(pluginsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"smtp.json", "email.json", "ssl.json"} {
		if err := os.Remove(filepath.Join(root, "config", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	pluginsAfter, err := os.ReadFile(pluginsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pluginsBefore, pluginsAfter) {
		t.Fatal("resume replaced an equivalent existing secret section")
	}
	if _, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyConfigRejectsInconsistentExistingSectionBeforeWriting(t *testing.T) {
	root := t.TempDir()
	original := writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":        "smail.db",
		"dbType":       "sqlite",
		"domain":       "mail.example.com",
		"domains":      []string{"mail.example.com"},
		"webDomain":    "mail.example.com",
		"sslType":      "0",
		"isInit":       true,
		"httpsEnabled": 1,
		"httpPort":     80,
		"httpsPort":    443,
	})
	conflicting := []byte(`{"meta":{"schema":1,"generation":1},"policy":{"smtpPort":2525}}`)
	conflictingPath := filepath.Join(root, "config", "email.json")
	if err := os.WriteFile(conflictingPath, conflicting, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); !errors.Is(err, ErrMigrationTargetExists) {
		t.Fatalf("error = %v", err)
	}
	current, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("conflicting migration changed the legacy root configuration")
	}
	stored, err := os.ReadFile(conflictingPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, conflicting) {
		t.Fatal("conflicting section was overwritten")
	}
	for _, name := range []string{"smtp.json", "ssl.json", "plugins.json"} {
		if _, err := os.Stat(filepath.Join(root, "config", name)); !os.IsNotExist(err) {
			t.Fatalf("conflicting migration created %s", name)
		}
	}
}

func mustMigrationVault(t *testing.T, root string) *credential.Vault {
	t.Helper()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	return vault
}

func TestMigrateLegacyConfigMapsPluginAllowListToFixedLogicalIDs(t *testing.T) {
	root := t.TempDir()
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":                "smail.db",
		"dbType":               "sqlite",
		"domain":               "mail.example.com",
		"domains":              []string{"mail.example.com"},
		"webDomain":            "mail.example.com",
		"sslType":              "0",
		"isInit":               true,
		"httpsEnabled":         1,
		"httpPort":             80,
		"httpsPort":            443,
		"weChatPushAppId":      "app-id",
		"weChatPushSecret":     "wechat-secret",
		"weChatPushTemplateId": "template-id",
		"tgBotToken":           "telegram-token",
		"pluginAllowList":      []string{"spam_block.exe", "wechat_push", "pmail_telegram_push.exe"},
	})

	if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, logicalID := range []string{"spam_block", "wechat_push", "telegram_push"} {
		plugin, ok := runtime.Plugins.Plugins[logicalID]
		if !ok || !plugin.Enabled {
			t.Fatalf("plugin %s was not enabled from the legacy allow list: %+v", logicalID, runtime.Plugins.Plugins)
		}
	}
}

func TestPlanLegacyMigrationRejectsUnsafePluginAllowList(t *testing.T) {
	cases := map[string][]string{
		"路径":    {"../spam_block"},
		"大小写冲突": {"Spam_Block"},
		"未知插件":  {"unknown_plugin"},
		"逻辑重复":  {"spam_block", "spam_block.exe"},
	}
	for name, allowList := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeLegacyMigrationFixture(t, root, map[string]any{
				"dbDSN":           "smail.db",
				"dbType":          "sqlite",
				"pluginAllowList": allowList,
			})
			plan, err := PlanLegacyMigration(root, RuntimeValidationOptions{})
			if !errors.Is(err, ErrMigrationUnsafeValue) {
				t.Fatalf("error = %v", err)
			}
			if len(plan.ManualFields) != 1 || plan.ManualFields[0] != "pluginAllowList" {
				t.Fatalf("manual fields = %+v", plan.ManualFields)
			}
		})
	}
}

func TestPlanLegacyMigrationRejectsEnabledPluginWithoutRequiredCredential(t *testing.T) {
	root := t.TempDir()
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":           "smail.db",
		"dbType":          "sqlite",
		"pluginAllowList": []string{"wechat_push"},
	})
	plan, err := PlanLegacyMigration(root, RuntimeValidationOptions{})
	if !errors.Is(err, ErrMigrationUnsafeValue) {
		t.Fatalf("error = %v", err)
	}
	if len(plan.ManualFields) != 1 || plan.ManualFields[0] != "weChatPushSecret" {
		t.Fatalf("manual fields = %+v", plan.ManualFields)
	}
}

func TestMigrateLegacyConfigParsesKnownStructuredDatabaseDSNs(t *testing.T) {
	cases := []struct {
		name     string
		database string
		dsn      string
		wantType DatabaseType
		wantPort uint16
		wantHost string
		wantName string
		password string
	}{
		{
			name:     "MySQL",
			database: DBTypeMySQL,
			dsn:      "smail:database-secret@tcp(db.example.com:3307)/smail?charset=utf8mb4",
			wantType: DatabaseTypeMySQL,
			wantPort: 3307,
			wantHost: "db.example.com",
			wantName: "smail",
			password: "database-secret",
		},
		{
			name:     "PostgreSQL",
			database: DBTypePostgres,
			dsn:      "postgres://smail:database-secret@db.example.com:5433/smail",
			wantType: DatabaseTypePostgreSQL,
			wantPort: 5433,
			wantHost: "db.example.com",
			wantName: "smail",
			password: "database-secret",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeLegacyMigrationFixture(t, root, map[string]any{
				"dbDSN":        testCase.dsn,
				"dbType":       testCase.database,
				"domain":       "mail.example.com",
				"domains":      []string{"mail.example.com"},
				"webDomain":    "mail.example.com",
				"sslType":      "0",
				"isInit":       true,
				"httpsEnabled": 1,
				"httpPort":     80,
				"httpsPort":    443,
			})
			if _, err := MigrateLegacyConfig(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
				t.Fatal(err)
			}
			runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
			if err != nil {
				t.Fatal(err)
			}
			database := runtime.Root.Database
			if database.Type != testCase.wantType || database.Port != testCase.wantPort || database.Host != testCase.wantHost || database.Name != testCase.wantName || database.Username != "smail" {
				t.Fatalf("database = %+v", database)
			}
			opened, err := OpenStoredSecret(mustMigrationVault(t, root), database.Password)
			if err != nil {
				t.Fatal(err)
			}
			if opened != testCase.password {
				t.Fatalf("database password = %q", opened)
			}
			data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(testCase.password)) || bytes.Contains(data, []byte(testCase.dsn)) {
				t.Fatal("database credential leaked to the migrated configuration")
			}
		})
	}
}

func TestPlanLegacyMigrationRejectsAmbiguousDatabaseDSN(t *testing.T) {
	root := t.TempDir()
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":  "postgres://smail:password@db.example.com:5432/smail?sslmode=disable",
		"dbType": DBTypePostgres,
	})
	plan, err := PlanLegacyMigration(root, RuntimeValidationOptions{})
	if !errors.Is(err, ErrMigrationUnsafeValue) {
		t.Fatalf("error = %v", err)
	}
	if len(plan.ManualFields) != 1 || plan.ManualFields[0] != "dbDSN" {
		t.Fatalf("manual fields = %+v", plan.ManualFields)
	}
}

func TestMigrateLegacyConfigDerivesSetupStateFromFullPreflight(t *testing.T) {
	root := t.TempDir()
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbDSN":        "smail.db",
		"dbType":       "sqlite",
		"domain":       "mail.example.com",
		"domains":      []string{"mail.example.com"},
		"webDomain":    "mail.example.com",
		"sslType":      "0",
		"isInit":       true,
		"httpsEnabled": 1,
		"httpPort":     80,
		"httpsPort":    443,
	})
	options := RuntimeValidationOptions{HasSuperAdmin: false, Production: true}
	if _, err := MigrateLegacyConfig(root, options); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: options}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.SetupState != SetupStateDegraded || runtime.Root.SetupState != SetupStateDegraded {
		t.Fatalf("setup state = %q / %q", runtime.SetupState, runtime.Root.SetupState)
	}
}
