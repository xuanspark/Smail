package dbmigration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

// 使用正式部署布局和配置管理器，避免把替代回调误认为配置事务验收。
func portableIntegrationManager(t *testing.T, root string, source config.DatabaseConfig) *config.ConfigManager {
	t.Helper()
	if err := config.InitializeDeployment(root); err != nil {
		t.Fatal("integration deployment initialization failed")
	}
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	meta := config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 1}
	tls := config.SSLConfig{Mode: "default", DefaultDirectory: "ssl"}
	sections := map[config.Section]any{
		config.SectionSMTP: config.SMTPSectionConfig{Meta: meta},
		config.SectionEmail: config.EmailSectionConfig{Meta: meta, Policy: config.EmailConfig{
			SMTPPort: 25, IMAPPort: 143, POP3Port: 110,
		}},
		config.SectionSSL:     config.SSLSectionConfig{Meta: meta, TLS: tls},
		config.SectionPlugins: config.PluginsSectionConfig{Meta: meta, Plugins: map[string]config.PluginInstance{}},
	}
	references := map[config.Section]config.SectionRef{}
	for section, value := range sections {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		name, _ := config.FixedSectionFile(section)
		schema, _ := config.FixedSectionSchema(section)
		references[section] = config.SectionRef{File: name, Schema: schema, Generation: 1, SHA256: config.SectionDigest(data)}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	settings := config.RootConfig{
		SchemaVersion: config.CurrentSchemaVersion, Generation: 1, SetupState: config.SetupStateReady,
		Database: source, Sections: references, SSL: tls,
		Domains:  config.DomainConfig{Primary: "runtime.invalid", Web: "mail.runtime.invalid", Accepted: []string{"runtime.invalid"}},
		HTTP:     config.HTTPConfig{Port: 80, HTTPSPort: 443, HTTPSEnabled: true},
		Security: config.SecurityConfig{PasswordVersion: "argon2id"},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manager := reopenPortableManager(t, root)
	store := config.NewTransactionStore(root, config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if _, err := store.CreateBackup(manager.Snapshot()); err != nil {
		t.Fatal("integration configuration backup failed")
	}
	return manager
}

func reopenPortableManager(t *testing.T, root string) *config.ConfigManager {
	t.Helper()
	manager, err := config.NewConfigManagerWithSecurity(root,
		config.ConfigLoaderOptions{Validation: config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}},
		config.ConfigSecurity{Audit: &config.MemoryAuditWriter{}})
	if err != nil {
		t.Fatal("integration configuration manager could not reopen")
	}
	return manager
}

func TestPortableSQLiteWithRealConfigurationSwitch(t *testing.T) {
	runPortableIntegrationChain(t, nil)
}
