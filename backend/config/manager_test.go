package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRootConfig() RootConfig {
	return RootConfig{SchemaVersion: CurrentSchemaVersion, Generation: 1, SetupState: SetupStateBootstrap,
		Database: DatabaseConfig{Type: DatabaseTypeSQLite, LocalRef: "smail.db"},
		SMTP:     SMTPConfig{VerificationEnabled: false},
		SSL:      SSLConfig{Mode: "default", DefaultDirectory: "config/ssl"},
		Security: SecurityConfig{PasswordVersion: "argon2id"}}
}

func TestManagerUsesGenerationConflictAndBackupRetention(t *testing.T) {
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manager.Snapshot()
	candidate := snapshot.Config
	candidate.SetupState = SetupStateReady
	if _, err := manager.Apply(candidate, snapshot.Generation+1); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("conflict = %v", err)
	}
	updated, err := manager.Apply(candidate, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	path, err := BackupConfig(t.TempDir(), updated, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBackup(path); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRetentionKeepsFourRecentFiles(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		snapshot := manager.Snapshot()
		path, err := BackupConfig(root, snapshot, time.Now().Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(path) == "" {
			t.Fatal("backup path is empty")
		}
	}
}

func TestManagerSnapshotsAreDeepCopies(t *testing.T) {
	initial := testRootConfig()
	initial.Plugins = []PluginConfig{{LogicalID: "telegram", FileName: "telegram.so", NetworkAllowlist: []string{"api.example.com"}}}
	manager, err := NewManager(initial)
	if err != nil {
		t.Fatal(err)
	}
	initial.Plugins[0].NetworkAllowlist[0] = "attacker.example.com"
	snapshot := manager.Snapshot()
	if snapshot.Config.Plugins[0].NetworkAllowlist[0] != "api.example.com" {
		t.Fatal("manager retained caller-owned plugin slice")
	}
	snapshot.Config.Plugins[0].NetworkAllowlist[0] = "changed.example.com"
	if manager.Snapshot().Config.Plugins[0].NetworkAllowlist[0] != "api.example.com" {
		t.Fatal("snapshot exposed manager-owned plugin slice")
	}
}

func TestPersistentManagerAppliesBackupAndReloads(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	candidate := manager.Snapshot().Config
	candidate.SetupState = SetupStateReady
	updated, backup, err := manager.ApplyPersisted(root, candidate, 1, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != 2 || backup == "" {
		t.Fatalf("unexpected persisted update: generation=%d backup=%q", updated.Generation, backup)
	}
	loaded, err := LoadManager(root, testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := loaded.Snapshot(); snapshot.Generation != 2 || snapshot.Config.SetupState != SetupStateReady {
		t.Fatalf("reloaded snapshot = %+v", snapshot)
	}
}

func TestPersistentManagerRejectsSymlinkConfigDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "config")); err != nil {
		t.Skipf("?????????????: %v", err)
	}
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	candidate := manager.Snapshot().Config
	if _, _, err := manager.ApplyPersisted(root, candidate, 1, time.Now()); err == nil {
		t.Fatal("????????????")
	}
}
