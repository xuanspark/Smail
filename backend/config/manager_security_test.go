package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerApplySynchronizesConfigGeneration(t *testing.T) {
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	candidate := manager.Snapshot().Config
	candidate.Generation = 999
	candidate.SetupState = SetupStateReady
	updated, err := manager.Apply(candidate, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != 2 || updated.Config.Generation != updated.Generation {
		t.Fatalf("inconsistent generation: %+v", updated)
	}
	current := manager.Snapshot()
	if current.Generation != 2 || current.Config.Generation != 2 {
		t.Fatalf("inconsistent current snapshot: %+v", current)
	}
}

func TestManagerRollbackCreatesANewGeneration(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	candidate := manager.Snapshot().Config
	candidate.SetupState = SetupStateReady
	updated, backupPath, err := manager.ApplyPersisted(root, candidate, 1, time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, rollbackBackup, err := manager.RollbackPersisted(root, backupPath, updated.Generation, time.Date(2026, 8, 26, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Generation != 3 || rolledBack.Config.Generation != 3 || rolledBack.Config.SetupState != SetupStateBootstrap {
		t.Fatalf("unexpected rollback snapshot: %+v", rolledBack)
	}
	if rollbackBackup == "" || rollbackBackup == backupPath {
		t.Fatalf("unexpected rollback backup path: %q", rollbackBackup)
	}
}

func TestManagerRollbackRejectsBackupOutsideManagedDirectory(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "config-00000000000000000001-outside.json")
	data, err := json.Marshal(manager.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.RollbackPersisted(root, outside, 1, time.Now()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("outside backup error = %v", err)
	}
}

func TestLoadManagerRewritesLegacyVersion(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := testRootConfig()
	legacy.SchemaVersion = 0
	legacy.Generation = 0
	legacy.Version = 7
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := LoadManager(root, testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := manager.Snapshot(); snapshot.Generation != 7 || snapshot.Config.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("legacy snapshot = %+v", snapshot)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved RootConfig
	if err := json.Unmarshal(rewritten, &saved); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), `"version"`) || saved.SchemaVersion != CurrentSchemaVersion || saved.Generation != 7 {
		t.Fatal("旧配置未正确更新版本和代次")
	}
}

func TestManagerRejectsFutureSchemaVersion(t *testing.T) {
	candidate := testRootConfig()
	candidate.SchemaVersion = CurrentSchemaVersion + 1
	if _, err := NewManager(candidate); err == nil {
		t.Fatal("future schema version was accepted")
	}
}

func TestLoadManagerRejectsSymlinkConfigFile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	data, err := json.Marshal(testRootConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "config.json")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := LoadManager(root, testRootConfig()); err == nil {
		t.Fatal("symlink configuration file was accepted")
	}
}

func TestBackupRejectsSymlinkDirectoryAndEntry(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "data"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(root, "data", "backups")); err != nil {
			t.Skipf("symlink is unavailable: %v", err)
		}
		if _, err := BackupConfig(root, RuntimeSnapshot{Config: testRootConfig(), Generation: 1}, time.Now()); err == nil {
			t.Fatal("symlink backup directory was accepted")
		}
	})
	t.Run("entry", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, "data", "backups")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(directory, "config-malicious.json")); err != nil {
			t.Skipf("symlink is unavailable: %v", err)
		}
		if _, err := BackupConfig(root, RuntimeSnapshot{Config: testRootConfig(), Generation: 1}, time.Now()); err == nil {
			t.Fatal("symlink backup entry was accepted")
		}
	})
}

func TestLoadBackupRejectsGenerationMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.json")
	snapshot := RuntimeSnapshot{Config: testRootConfig(), Generation: 2}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBackup(path); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("generation mismatch error = %v", err)
	}
}

func TestBackupRetentionKeepsExactlyFourRecentManagedFiles(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	for generation := uint64(1); generation <= 6; generation++ {
		cfg := testRootConfig()
		cfg.Generation = generation
		_, err := BackupConfig(root, RuntimeSnapshot{Config: cfg, Generation: generation}, base.Add(time.Duration(generation)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("backup count = %d", len(entries))
	}
}
