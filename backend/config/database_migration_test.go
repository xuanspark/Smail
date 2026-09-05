package config

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConfigManagerSwitchDatabaseCommitsSQLiteReference(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}

	target := manager.Snapshot().Root.Database
	target.LocalRef = "smail-next.db"
	if err := manager.SwitchDatabase(context.Background(), 1, target, authorizedConfigActor()); err != nil {
		t.Fatal(err)
	}
	if got := manager.Snapshot(); got.Generation != 2 || got.Root.Database.LocalRef != "smail-next.db" {
		t.Fatalf("switched snapshot = %+v", got)
	}
	status := manager.Status()
	if len(status.PendingRestart) != 1 || status.PendingRestart[0] != ServiceDatabase {
		t.Fatalf("pending restart services = %+v", status.PendingRestart)
	}
	audit := manager.security.Audit.(*FileAuditWriter)
	events, err := audit.Query(context.Background(), ConfigAuditQuery{Operation: ConfigOperationMigration, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ChangedFields[0] != "root.database.localRef" {
		t.Fatalf("migration audit events = %+v", events)
	}
}

func TestConfigManagerSwitchAcrossTypesPersistsAndRedactsAudit(t *testing.T) {
	for _, kind := range []DatabaseType{DatabaseTypeMySQL, DatabaseTypePostgreSQL} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			writeRuntimeFixture(t, root, validRuntimeConfig())
			options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
			manager, err := NewConfigManager(root, options)
			if err != nil {
				t.Fatal(err)
			}
			secret, err := ApplySecretChange(manager.vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: "migration-test-secret"}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			target := NormalizeDatabaseConfig(DatabaseConfig{Type: kind, Host: "127.0.0.1", Username: "review", Name: "smail_next", Password: secret})
			if err := manager.SwitchDatabase(context.Background(), 1, target, authorizedConfigActor()); err != nil {
				t.Fatal(err)
			}
			reloaded, err := NewConfigManager(root, options)
			if err != nil {
				t.Fatal(err)
			}
			if got := reloaded.Snapshot(); got.Generation != 2 || got.Root.Database != target {
				t.Fatal("target configuration was not persisted")
			}
			events, err := manager.security.Audit.(*FileAuditWriter).Query(context.Background(), ConfigAuditQuery{Operation: ConfigOperationMigration, Limit: 10})
			if err != nil || len(events) != 1 {
				t.Fatal("migration audit missing")
			}
			data, _ := json.Marshal(events)
			if strings.Contains(string(data), "migration-test-secret") || strings.Contains(string(data), secret.Ciphertext) || events[0].ChangedFields[0] != "root.database" {
				t.Fatal("migration audit disclosed secret or omitted connection change")
			}
		})
	}
}

func TestConfigManagerSwitchDatabaseRejectsImmutableChangesAndStaleGeneration(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	target := manager.Snapshot().Root.Database
	target.LocalRef = "next.db"
	target.Host = "127.0.0.1"
	if err := manager.SwitchDatabase(context.Background(), 1, target, authorizedConfigActor()); !errors.Is(err, ErrDatabaseMigrationTarget) {
		t.Fatalf("immutable target error = %v, want invalid migration target", err)
	}
	target = manager.Snapshot().Root.Database
	target.LocalRef = "next.db"
	if err := manager.SwitchDatabase(context.Background(), 2, target, authorizedConfigActor()); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("stale generation error = %v, want generation conflict", err)
	}
}

func TestConfigManagerSwitchDatabaseRequiresAuthorizedActor(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	target := manager.Snapshot().Root.Database
	target.LocalRef = "next.db"
	if err := manager.SwitchDatabase(context.Background(), 1, target, AuditActor{}); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("missing actor error = %v, want authorization required", err)
	}
}

func TestConfigManagerSwitchDatabaseHonorsCancellationBeforePersistence(t *testing.T) {
	root := t.TempDir()
	writeRuntimeFixture(t, root, validRuntimeConfig())
	manager, err := NewConfigManager(root, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}})
	if err != nil {
		t.Fatal(err)
	}
	target := manager.Snapshot().Root.Database
	target.LocalRef = "next.db"
	if err := manager.SwitchDatabase(newCancelAfterFirstContext(), 1, target, authorizedConfigActor()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled database switch error = %v, want context canceled", err)
	}
	if manager.Snapshot().Generation != 1 || manager.Snapshot().Root.Database.LocalRef != "smail.db" {
		t.Fatalf("canceled database switch changed snapshot: %+v", manager.Snapshot())
	}
}
