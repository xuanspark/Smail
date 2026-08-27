package config

import "testing"

func TestRootConfigValidatesStructuredDatabaseAndPluginID(t *testing.T) {
	root := RootConfig{
		SchemaVersion: CurrentSchemaVersion, Generation: 1, SetupState: SetupStateBootstrap,
		Database: DatabaseConfig{Type: DBTypeSQLite, SQLiteLocalRef: "smail.db"},
		SMTP:     SMTPConfig{VerificationEnabled: false},
		SSL:      SSLConfig{Mode: "default", DefaultDirectory: "config/ssl"},
		Plugins:  []PluginConfig{{LogicalID: "telegram_push", FileName: "telegram_push", NetworkAllowlist: []string{"api.telegram.org"}}},
		Security: SecurityConfig{PasswordVersion: "argon2id"},
	}
	if err := ValidateRootConfig(root); err != nil {
		t.Fatal(err)
	}
	root.Plugins[0].LogicalID = "../escape"
	if err := ValidateRootConfig(root); err == nil {
		t.Fatal("invalid plugin ID was accepted")
	}
}
