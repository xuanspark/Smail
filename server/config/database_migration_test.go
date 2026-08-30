package config

import (
	"context"
	"errors"
	"testing"
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
