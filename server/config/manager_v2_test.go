package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

func writeRuntimeFixture(t *testing.T, root string, runtime RuntimeConfig) {
	t.Helper()
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := marshalRuntimeFilesWithPluginSecrets(runtime, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := atomicfile.Write(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func validMySQLRuntimeConfig(t *testing.T, root string) RuntimeConfig {
	t.Helper()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("initial-database-secret")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Root.Database = DatabaseConfig{
		Type:     DatabaseTypeMySQL,
		Host:     "127.0.0.1",
		Port:     DefaultMySQLPort,
		Username: "smail",
		Password: StoredSecret{Ciphertext: ciphertext, Version: vault.Version(), UpdatedAt: time.Now().UTC()},
		Name:     "smail",
	}
	return runtime
}
func authorizedConfigActor() AuditActor {
	return AuditActor{UserID: "1", Role: "super_admin", SessionID: "test-session", RecentAuth: true, RecoveryCodeConfigured: true}
}

func TestConfigLoaderLoadsFixedPartitionsAndRejectsDigestMismatch(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	runtime, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Generation != 1 || runtime.SMTP.Meta.Generation != 1 || runtime.Email.Meta.Generation != 1 {
		t.Fatalf("unexpected runtime generation: %+v", runtime)
	}
	sectionPath := filepath.Join(root, "config", "email.json")
	data, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = ' '
	if err := os.WriteFile(sectionPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(); !errors.Is(err, ErrConfigDigestMismatch) {
		t.Fatalf("load error = %v, want digest mismatch", err)
	}
}

func TestConfigLoaderRejectsMissingPartitionAndGenerationMismatch(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	if err := os.Remove(filepath.Join(root, "config", "ssl.json")); err != nil {
		t.Fatal(err)
	}
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if _, err := loader.Load(); !errors.Is(err, ErrConfigSectionMissing) {
		t.Fatalf("load error = %v, want missing section", err)
	}

	root = t.TempDir()
	runtime := validRuntimeConfig()
	runtime.Generation = 2
	runtime.Root.Generation = 2
	runtime.SMTP.Meta.Generation = 2
	runtime.Email.Meta.Generation = 2
	runtime.SSL.Meta.Generation = 2
	runtime.Plugins.Meta.Generation = 2
	writeRuntimeFixture(t, root, runtime)
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootConfig RootConfig
	if err := ParseStrict(data, &rootConfig); err != nil {
		t.Fatal(err)
	}
	rootConfig.Sections[SectionEmail] = SectionRef{File: "email.json", Schema: CurrentSectionSchema, Generation: 1, SHA256: rootConfig.Sections[SectionEmail].SHA256}
	data, err = json.Marshal(rootConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load(); !errors.Is(err, ErrSectionGenerationMismatch) {
		t.Fatalf("load error = %v, want generation mismatch", err)
	}
}

func TestConfigManagerUpdateApplyUsesPendingGeneration(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	email := current.Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pending || updated.Generation != current.Generation+1 {
		t.Fatalf("unexpected update result: %+v", updated)
	}
	if _, err := manager.Validate(context.Background(), ValidateRequest{Section: SectionEmail, ExpectedGeneration: current.Generation, Payload: payload}); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("second update error = %v, want generation conflict", err)
	}
	applied, err := manager.Apply(context.Background(), updated.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Generation != 2 || manager.Snapshot().Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatalf("unexpected applied result: %+v", applied)
	}
	if _, err := manager.Apply(context.Background(), applied.Generation); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("second apply error = %v, want generation conflict", err)
	}
}

func TestTransactionRecoveryRestoresOldGenerationAfterSectionFailure(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	candidate.Generation = 2
	candidate.Root.Generation = 2
	candidate.SetupState = candidate.Root.SetupState
	candidate.SMTP.Meta.Generation = 2
	candidate.Email.Meta.Generation = 2
	candidate.SSL.Meta.Generation = 2
	candidate.Plugins.Meta.Generation = 2
	candidate.Email.Policy.MaxMessageBytes = 9 << 20
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionSectionsCommitted {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	if _, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != current.Generation || loaded.Email.Policy.MaxMessageBytes == 9<<20 {
		t.Fatalf("recovery did not restore old generation: %+v", loaded)
	}
}

func TestConfigManagerRollbackRestoresBackupAsNewGeneration(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	if manager.Snapshot().Email.Policy.MaxMessageBytes != 8<<20 {
		t.Fatal("updated configuration was not published")
	}
	rolledBack, err := manager.RollbackAuthorized(context.Background(), 1, authorizedConfigActor(), StepUpConfirmation{Confirmed: true, Method: "recent_auth"})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Generation != 3 {
		t.Fatalf("rollback generation = %d, want 3", rolledBack.Generation)
	}
	if rolledBack.Snapshot.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatal("rollback did not restore the selected backup")
	}
	loaded, err := NewConfigLoader(root, options).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != 3 || loaded.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("persisted rollback is invalid: %+v", loaded)
	}
}

func TestConfigBackupRejectsTamperedPartition(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if _, err := store.CreateBackup(runtime); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "config", "backups-v2"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup entries = %v, err = %v", entries, err)
	}
	path := filepath.Join(root, "config", "backups-v2", entries[0].Name(), "files", "email.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBackup(1); !errors.Is(err, ErrConfigDigestMismatch) {
		t.Fatalf("tampered backup error = %v, want digest mismatch", err)
	}
}

func TestConfigBackupRetentionKeepsFourRecentBackups(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	for generation := uint64(1); generation <= 5; generation++ {
		runtime.Generation = generation
		setCandidateGeneration(&runtime, generation)
		if _, err := store.CreateBackup(runtime); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "config", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("backup count = %d, want 4", len(entries))
	}
}

func TestTransactionRecoveryKeepsOrRestoresByPhase(t *testing.T) {
	phases := []struct {
		name    string
		phase   TransactionPhase
		wantNew bool
	}{
		{name: "prepared", phase: TransactionPrepared, wantNew: false},
		{name: "sections_committed", phase: TransactionSectionsCommitted, wantNew: false},
		{name: "root_committed", phase: TransactionRootCommitted, wantNew: true},
		{name: "verified", phase: TransactionVerified, wantNew: true},
	}
	for _, test := range phases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			candidate.Email.Policy.MaxMessageBytes = 9 << 20
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
				BeforePhase: func(phase TransactionPhase) error {
					if phase == test.phase {
						return errors.New("injected interruption")
					}
					return nil
				},
			})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("transaction unexpectedly succeeded")
			}
			if _, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
			if err != nil {
				t.Fatal(err)
			}
			gotNew := loaded.Email.Policy.MaxMessageBytes == 9<<20
			if gotNew != test.wantNew {
				t.Fatalf("recovery new=%v, want %v; generation=%d", gotNew, test.wantNew, loaded.Generation)
			}
		})
	}
}

func TestConfigManagerSecretChangesRequireStepUpAndPreserveStoredSecret(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	rootValue := manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootPayload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	request := UpdateRequest{
		Section:            SectionRoot,
		ExpectedGeneration: 1,
		Payload:            rootPayload,
		SecretChanges: map[string]SecretChange{
			"database.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "database-secret"}},
		},
		Actor: authorizedConfigActor(),
	}
	if _, err := manager.Update(context.Background(), request); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("replace without step-up error = %v, want step-up required", err)
	}
	request.Confirmation = StepUpConfirmation{Confirmed: true, Method: "recent_auth"}
	updated, err := manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Candidate.Root.Database.Password.Configured() {
		t.Fatal("replacement did not configure database secret")
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	stored := manager.Snapshot().Root.Database.Password
	rootValue = manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootValue.Database.Name = "next"
	payload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	request = UpdateRequest{Section: SectionRoot, ExpectedGeneration: 2, Payload: payload, Actor: authorizedConfigActor()}
	updated, err = manager.Update(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Candidate.Root.Database.Password != stored {
		t.Fatalf("stored secret changed without explicit secret operation: got %+v want %+v", updated.Candidate.Root.Database.Password, stored)
	}
}

func TestConfigManagerSecretClearRequiresExplicitConfirmation(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	runtime.SMTP.Relay = SMTPConfig{}
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	smtp := manager.Snapshot().SMTP
	payload, err := json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	replaceRequest := UpdateRequest{
		Section: SectionSMTP, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
		SecretChanges: map[string]SecretChange{"relay.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "relay-secret"}}},
		Confirmation:  StepUpConfirmation{Confirmed: true, Method: "recent_auth"},
	}
	updated, err := manager.Update(context.Background(), replaceRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	smtp = manager.Snapshot().SMTP
	smtp.Relay.Password = StoredSecret{}
	payload, err = json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	clearRequest := UpdateRequest{
		Section: SectionSMTP, ExpectedGeneration: 2, Payload: payload, Actor: authorizedConfigActor(),
		SecretChanges: map[string]SecretChange{"relay.password": {Operation: SecretClear, Confirmed: true}},
	}
	if _, err := manager.Update(context.Background(), clearRequest); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("clear without step-up error = %v, want step-up required", err)
	}
	clearRequest.Confirmation = StepUpConfirmation{Confirmed: true, Method: "webauthn"}
	updated, err = manager.Update(context.Background(), clearRequest)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Candidate.SMTP.Relay.Password.Configured() {
		t.Fatal("explicit clear left the relay secret configured")
	}
}

func TestConfigManagerSecretChangesUseVaultAndRejectUnknownFields(t *testing.T) {
	root := t.TempDir()
	runtime := validMySQLRuntimeConfig(t, root)
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	rootValue := manager.Snapshot().Root
	rootValue.Database.Password = StoredSecret{}
	rootPayload, err := json.Marshal(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Update(context.Background(), UpdateRequest{
		Section:            SectionRoot,
		ExpectedGeneration: 1,
		Payload:            rootPayload,
		SecretChanges: map[string]SecretChange{
			"database.password": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "database-secret"}},
		},
		Actor:        authorizedConfigActor(),
		Confirmation: StepUpConfirmation{Confirmed: true, Method: "recent_auth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := result.Candidate.Root.Database.Password
	if !stored.Configured() || stored.Ciphertext == "database-secret" {
		t.Fatalf("secret was not encrypted: %+v", stored)
	}
	if _, err := manager.Validate(context.Background(), ValidateRequest{
		Section:       SectionRoot,
		Payload:       rootPayload,
		SecretChanges: map[string]SecretChange{"database.unknown": {Operation: SecretKeep}},
	}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("unknown secret field error = %v, want invalid secret change", err)
	}
}

func TestConfigManagerRejectsPluginSecretWrites(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	writeRuntimeFixture(t, root, runtime)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}

	plugins := manager.Snapshot().Plugins
	payload, err := json.Marshal(plugins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Validate(context.Background(), ValidateRequest{
		Section:       SectionPlugins,
		Payload:       payload,
		SecretChanges: map[string]SecretChange{"plugins.telegram_push.secret": {Operation: SecretReplace, Value: &SecretInput{Plaintext: "plugin-secret"}}},
	}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("plugin secret update error = %v, want invalid secret change", err)
	}

	plugin := plugins.Plugins["telegram_push"]
	plugin.Secret = StoredSecret{Ciphertext: "smail:v1:encrypted", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()}
	plugins.Plugins["telegram_push"] = plugin
	payload, err = json.Marshal(plugins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Validate(context.Background(), ValidateRequest{Section: SectionPlugins, Payload: payload}); !errors.Is(err, ErrInvalidSecretChange) {
		t.Fatalf("direct plugin secret error = %v, want invalid secret change", err)
	}
}

func TestConfigManagerMigratesLegacyPluginSecrets(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "legacy-token"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = stored
	runtime.Plugins.Plugins["telegram_push"] = plugin
	writeRuntimeFixture(t, root, runtime)

	writer := &MemoryAuditWriter{}
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: writer})
	if err != nil {
		t.Fatal(err)
	}
	var imported []LegacyPluginSecret
	migrated, err := manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, values []LegacyPluginSecret) error {
		imported = append([]LegacyPluginSecret(nil), values...)
		return nil
	})
	if err != nil || !migrated {
		t.Fatalf("migrate legacy plugin secrets = %v, %v", migrated, err)
	}
	if len(imported) != 1 || imported[0].PluginID != "telegram_push" || imported[0].FieldName != "botToken" || imported[0].Plaintext != "legacy-token" {
		t.Fatalf("imported legacy values = %+v", imported)
	}
	if manager.Snapshot().Generation != 2 || manager.Snapshot().Plugins.Plugins["telegram_push"].Secret.Configured() {
		t.Fatalf("migrated snapshot = %+v", manager.Snapshot())
	}
	pluginData, err := os.ReadFile(filepath.Join(root, "config", "plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pluginData) == "" || containsAny(string(pluginData), "legacy-token", "ciphertext", "\"secret\"") {
		t.Fatalf("plugins.json retained legacy secret material: %s", pluginData)
	}
	events := writer.Events()
	if len(events) != 1 || events[0].Operation != ConfigOperationMigration || events[0].Section != SectionPlugins {
		t.Fatalf("migration audit events = %+v", events)
	}
	migrated, err = manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, values []LegacyPluginSecret) error {
		t.Fatalf("unexpected repeated importer invocation: %+v", values)
		return nil
	})
	if err != nil || migrated {
		t.Fatalf("repeat migration = %v, %v", migrated, err)
	}
}

func TestConfigManagerRetainsLegacySecretWhenImportFails(t *testing.T) {
	root := t.TempDir()
	runtime := validRuntimeConfig()
	vault, err := credential.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "legacy-token"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plugin := runtime.Plugins.Plugins["telegram_push"]
	plugin.Secret = stored
	runtime.Plugins.Plugins["telegram_push"] = plugin
	writeRuntimeFixture(t, root, runtime)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.MigrateLegacyPluginSecrets(context.Background(), func(_ context.Context, _ []LegacyPluginSecret) error {
		return errors.New("database unavailable")
	}); err == nil {
		t.Fatal("legacy import failure unexpectedly succeeded")
	}
	if manager.Snapshot().Generation != 1 || !manager.Snapshot().Plugins.Plugins["telegram_push"].Secret.Configured() {
		t.Fatal("failed import altered the in-memory legacy configuration")
	}
	pluginData, readErr := os.ReadFile(filepath.Join(root, "config", "plugins.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !containsAny(string(pluginData), "ciphertext", "\"secret\"") {
		t.Fatal("failed import removed compatibility secret from plugins.json")
	}
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func findTransactionDirectory(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "config", "transaction"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("transaction entries = %+v", entries)
	}
	return filepath.Join(root, "config", "transaction", entries[0].Name())
}

func TestTransactionRecoveryRejectsTamperedArtifacts(t *testing.T) {
	for _, test := range []struct {
		name string
		dir  string
	}{
		{name: "candidate", dir: "candidate"},
		{name: "original", dir: "original"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := validRuntimeConfig()
			writeRuntimeFixture(t, root, current)
			candidate := cloneRuntimeConfig(current)
			setCandidateGeneration(&candidate, 2)
			store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
				BeforePhase: func(phase TransactionPhase) error {
					if phase == TransactionPrepared {
						return errors.New("injected interruption")
					}
					return nil
				},
			})
			if err := store.Commit(current, candidate); err == nil {
				t.Fatal("transaction unexpectedly succeeded")
			}
			transactionDirectory := findTransactionDirectory(t, root)
			path := filepath.Join(transactionDirectory, test.dir, "email.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-1] ^= 1
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrConfigDigestMismatch) {
				t.Fatalf("tampered %s recovery error = %v, want digest mismatch", test.dir, err)
			}
		})
	}
}

func TestTransactionRecoveryRejectsArtifactSymlink(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	setCandidateGeneration(&candidate, 2)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionPrepared {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	transactionDirectory := findTransactionDirectory(t, root)
	candidateDirectory := filepath.Join(transactionDirectory, "candidate")
	originalDirectory := filepath.Join(transactionDirectory, "original")
	if err := os.RemoveAll(candidateDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(originalDirectory, candidateDirectory); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrTransactionInvalid) {
		t.Fatalf("symlink recovery error = %v, want invalid transaction", err)
	}
}

func TestTransactionRecoveryRestoresWhenCommittedRootIsDamaged(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	candidate := cloneRuntimeConfig(current)
	setCandidateGeneration(&candidate, 2)
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).WithHooks(TransactionHooks{
		BeforePhase: func(phase TransactionPhase) error {
			if phase == TransactionRootCommitted {
				return errors.New("injected interruption")
			}
			return nil
		},
	})
	if err := store.Commit(current, candidate); err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	path := filepath.Join(root, "config", "email.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewConfigLoader(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != current.Generation || loaded.Email.Policy.MaxMessageBytes != current.Email.Policy.MaxMessageBytes {
		t.Fatalf("recovered runtime = %+v, want generation %d", loaded, current.Generation)
	}
}

func TestTransactionRecoveryRejectsInvalidTransactionID(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := secureTransactionDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(transactions, "invalid-id"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}).Recover(); !errors.Is(err, ErrTransactionInvalid) {
		t.Fatalf("invalid transaction id error = %v, want invalid transaction", err)
	}
}

func TestConfigBackupScheduleUsesSevenDayInterval(t *testing.T) {
	root := t.TempDir()
	first := validRuntimeConfig()
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	firstAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path, created, err := store.CreateBackupIfDue(first, firstAt)
	if err != nil || !created || path == "" {
		t.Fatalf("first backup path=%q created=%v err=%v", path, created, err)
	}
	second := cloneRuntimeConfig(first)
	setCandidateGeneration(&second, 2)
	beforeDue, created, err := store.CreateBackupIfDue(second, firstAt.Add(7*24*time.Hour-time.Nanosecond))
	if err != nil || created || beforeDue != path {
		t.Fatalf("before due path=%q created=%v err=%v, want existing backup", beforeDue, created, err)
	}
	atDue, created, err := store.CreateBackupIfDue(second, firstAt.Add(7*24*time.Hour))
	if err != nil || !created || atDue == path {
		t.Fatalf("at due path=%q created=%v err=%v, want new backup", atDue, created, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "config", "backups-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("scheduled backup count=%d, want 2", len(entries))
	}
}

func TestConfigManagerStatusReportsPendingRestartServices(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Snapshot()
	smtp := current.SMTP
	smtp.Relay.RelayPort++
	payload, err := json.Marshal(smtp)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionSMTP, ExpectedGeneration: current.Generation, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceSMTP {
		t.Fatalf("pending restart services = %+v, want smtp", status.PendingRestart)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	status = manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceSMTP {
		t.Fatalf("applied pending restart services = %+v, want smtp", status.PendingRestart)
	}
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status = manager.Status(); len(status.PendingRestart) != 0 {
		t.Fatalf("loaded pending restart services = %+v, want empty", status.PendingRestart)
	}
}

type failingConfigAuditWriter struct{}

func (failingConfigAuditWriter) WriteConfigAudit(context.Context, ConfigAuditEvent) error {
	return errors.New("audit backend unavailable")
}

func TestConfigManagerRequiresActorForWrites(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload}); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("missing actor error = %v, want authorization required", err)
	}
}

func TestConfigManagerRollbackTicketIsBoundAndSingleUse(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
	manager, err := NewConfigManager(root, options)
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	preview, err := manager.PreviewRollback(context.Background(), 1, actor)
	if err != nil {
		t.Fatal(err)
	}
	if preview.CurrentGeneration != 2 || preview.TargetGeneration != 1 || preview.CandidateGeneration != 3 {
		t.Fatalf("unexpected rollback preview: %+v", preview)
	}
	ticket, err := manager.IssueRollbackTicket(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"})
	if err != nil {
		t.Fatal(err)
	}
	wrongActor := actor
	wrongActor.SessionID = "different-session"
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, wrongActor); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("wrong session error = %v, want authorization required", err)
	}
	rolledBack, err := manager.RollbackWithTicket(context.Background(), ticket, actor)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Generation != 3 || rolledBack.Snapshot.Email.Policy.MaxMessageBytes == 8<<20 {
		t.Fatalf("rollback result = %+v", rolledBack)
	}
	if _, err := manager.RollbackWithTicket(context.Background(), ticket, actor); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("replayed ticket error = %v, want authorization required", err)
	}
}

func TestConfigManagerRollbackRequiresStepUpButNotRecoveryCodeForSuperAdmin(t *testing.T) {
	root := t.TempDir()
	current := validRuntimeConfig()
	writeRuntimeFixture(t, root, current)
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	updatedEmail := manager.Snapshot().Email
	updatedEmail.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(updatedEmail)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	actor := authorizedConfigActor()
	actor.RecoveryCodeConfigured = false
	if _, err := manager.RollbackAuthorized(context.Background(), 1, actor, StepUpConfirmation{}); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("missing step-up error = %v, want step-up required", err)
	}
	if _, err := manager.RollbackAuthorized(context.Background(), 1, actor, StepUpConfirmation{Confirmed: true, Method: "webauthn"}); err != nil {
		t.Fatalf("rollback with WebAuthn confirmation failed: %v", err)
	}
}

func TestConfigManagerWritesRedactedAuditAndDegradesOnAuditFailure(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	writer := &MemoryAuditWriter{}
	manager, err := NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: writer})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); err != nil {
		t.Fatal(err)
	}
	events := writer.Events()
	if len(events) != 1 || events[0].Section != SectionEmail || len(events[0].ChangedFields) != 1 || events[0].ChangedFields[0] != string(SectionEmail) {
		t.Fatalf("unexpected audit events: %+v", events)
	}
	if events[0].Result != "success" || events[0].ToGeneration != 2 {
		t.Fatalf("unexpected audit event: %+v", events[0])
	}

	root = t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err = NewConfigManagerWithSecurity(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}, ConfigSecurity{Audit: failingConfigAuditWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(manager.Snapshot().Email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = manager.Update(context.Background(), UpdateRequest{Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), updated.Generation); !errors.Is(err, ErrAuditWrite) {
		t.Fatalf("audit failure error = %v, want audit write", err)
	}
	if manager.Status().SetupState != SetupStateDegraded {
		t.Fatalf("status after audit failure = %+v, want degraded", manager.Status())
	}
}

func TestConfigManagerSnapshotResultsAreIndependentCopies(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}

	current := manager.Snapshot()
	current.Root.Domains.Accepted[0] = "changed.example.com"
	current.Plugins.Plugins["telegram_push"].Settings[0] = '['
	if got := manager.Snapshot(); got.Root.Domains.Accepted[0] != "mail.example.com" || !json.Valid(got.Plugins.Plugins["telegram_push"].Settings) {
		t.Fatal("manager snapshot exposed active mutable data")
	}

	email := manager.Snapshot().Email
	email.Policy.MaxMessageBytes = 8 << 20
	payload, err := json.Marshal(email)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), UpdateRequest{
		Section: SectionEmail, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated.Candidate.Email.Policy.MaxMessageBytes = 64 << 20
	if got := manager.Snapshot(); got.Email.Policy.MaxMessageBytes == 64<<20 {
		t.Fatal("update result exposed manager-owned pending data")
	}

	applied, err := manager.Apply(context.Background(), updated.Generation)
	if err != nil {
		t.Fatal(err)
	}
	applied.Snapshot.Email.Policy.MaxMessageBytes = 128 << 20
	if got := manager.Snapshot(); got.Email.Policy.MaxMessageBytes == 128<<20 {
		t.Fatal("apply result exposed manager-owned active data")
	}
}
