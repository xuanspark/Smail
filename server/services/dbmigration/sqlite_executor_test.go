package dbmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	_ "modernc.org/sqlite"
)

func newSQLiteMigrationFixture(t *testing.T, switchConfig SQLiteSwitch) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	configDirectory := filepath.Join(root, "config")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(configDirectory, "smail.db")
	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE messages (id INTEGER PRIMARY KEY, body TEXT NOT NULL); INSERT INTO messages(body) VALUES ('测试邮件');`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	executor := NewSQLiteExecutor(root, switchConfig)
	return NewService(executor), root
}

func TestSQLiteExecutorCopiesDatabaseAndSwitchesConfiguration(t *testing.T) {
	var switched config.DatabaseConfig
	service, root := newSQLiteMigrationFixture(t, func(_ context.Context, candidate config.DatabaseConfig) error {
		switched = candidate
		return nil
	})
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 3, "smail-next.db")
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Execute(context.Background(), record.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusSucceeded {
		t.Fatalf("migration status = %s, want succeeded", completed.Status)
	}
	if switched.LocalRef != "smail-next.db" || switched.Type != config.DatabaseTypeSQLite {
		t.Fatalf("unexpected switched configuration: %+v", switched)
	}
	database, err := sql.Open("sqlite", filepath.Join(root, "config", "smail-next.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var body string
	if err := database.QueryRow("SELECT body FROM messages WHERE id = 1").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "测试邮件" {
		t.Fatalf("copied row = %q, want copied content", body)
	}
	info, err := os.Stat(filepath.Join(root, "config", "smail-next.db"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("migrated database permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestSQLiteExecutorRejectsExistingTarget(t *testing.T) {
	service, root := newSQLiteMigrationFixture(t, func(context.Context, config.DatabaseConfig) error { return nil })
	if err := os.WriteFile(filepath.Join(root, "config", "smail-next.db"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 1, "smail-next.db")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("execution error = %v, want target exists", err)
	}
	if failed.Status != StatusFailed || failed.ErrorCode != "TARGET_EXISTS" {
		t.Fatalf("unexpected failed migration: %+v", failed)
	}
}

func TestSQLiteExecutorRejectsCorruptSource(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "smail.db"), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	executor := NewSQLiteExecutor(root, func(context.Context, config.DatabaseConfig) error { return nil })
	plan := Plan{sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, targetConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "next.db"}}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); err == nil {
		t.Fatal("corrupt source unexpectedly migrated")
	} else if !errors.Is(err, ErrSQLiteIntegrity) {
		t.Fatalf("corrupt source error = %v, want integrity error", err)
	}
}

func TestSQLiteExecutorRejectsUnsupportedDatabase(t *testing.T) {
	executor := NewSQLiteExecutor(t.TempDir(), func(context.Context, config.DatabaseConfig) error { return nil })
	plan := Plan{sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeMySQL}, targetConfig: config.DatabaseConfig{Type: config.DatabaseTypeMySQL}}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); !errors.Is(err, ErrUnsupportedDatabase) {
		t.Fatalf("unsupported database error = %v", err)
	}
}

func TestSQLiteExecutorRequiresConfigurationSwitch(t *testing.T) {
	service, root := newSQLiteMigrationFixture(t, nil)
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 1, "next.db")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if !errors.Is(err, ErrSwitchUnavailable) {
		t.Fatalf("execution error = %v, want switch unavailable", err)
	}
	if failed.ErrorCode != "SWITCH_UNAVAILABLE" {
		t.Fatalf("error code = %q, want SWITCH_UNAVAILABLE", failed.ErrorCode)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "next.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target database remained after switch failure, stat error = %v", err)
	}
}

func TestSQLiteExecutorPreservesTargetWhenConfigurationSwitchOutcomeIsUnknown(t *testing.T) {
	service, root := newSQLiteMigrationFixture(t, func(context.Context, config.DatabaseConfig) error {
		return errors.New("配置切换失败")
	})
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 1, "next.db")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if err == nil || failed.Status != StatusFailed {
		t.Fatalf("switch failure result = %+v, error = %v", failed, err)
	}
	if !errors.Is(err, ErrSwitchOutcomeUnknown) {
		t.Fatalf("switch failure error = %v, want unknown switch outcome", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "config", "next.db")); statErr != nil {
		t.Fatalf("target database was removed after unknown switch outcome: %v", statErr)
	}
}

func TestSQLiteExecutorRejectsTargetSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 创建符号链接需要额外权限")
	}
	service, root := newSQLiteMigrationFixture(t, func(context.Context, config.DatabaseConfig) error { return nil })
	target := filepath.Join(root, "config", "next.db")
	if err := os.Symlink(filepath.Join(root, "config", "smail.db"), target); err != nil {
		t.Fatal(err)
	}
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 1, "next.db")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Execute(context.Background(), record.ID, token)
	if !errors.Is(err, ErrTargetExists) || failed.Status != StatusFailed {
		t.Fatalf("symlink target result = %+v, error = %v", failed, err)
	}
}

func TestSQLiteExecutorHonorsCancellation(t *testing.T) {
	service, _ := newSQLiteMigrationFixture(t, func(context.Context, config.DatabaseConfig) error { return nil })
	record, token, err := service.Preview(context.Background(), config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, 1, "next.db")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failed, err := service.Execute(ctx, record.ID, token)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("execution error = %v, want context canceled", err)
	}
	if failed.Status != StatusFailed {
		t.Fatalf("migration status = %s, want failed", failed.Status)
	}
}

func TestMigrationPlanDoesNotSerializePrivateDatabaseConfig(t *testing.T) {
	plan := Plan{Source: DatabaseIdentity{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}, Target: DatabaseIdentity{Type: config.DatabaseTypeSQLite, LocalRef: "next.db"}, sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"}}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"Source":{"type":"sqlite","localRef":"smail.db"},"Target":{"type":"sqlite","localRef":"next.db"}}` {
		t.Fatalf("serialized plan = %s", data)
	}
}
func TestSQLiteExecutorReportsOrderedStatuses(t *testing.T) {
	service, root := newSQLiteMigrationFixture(t, func(context.Context, config.DatabaseConfig) error { return nil })
	executor := service.runner.(*SQLiteExecutor)
	statuses := make([]Status, 0, 2)
	plan := Plan{
		sourceConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "smail.db"},
		targetConfig: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "next.db"},
	}
	if err := executor.Run(context.Background(), plan, func(status Status) error {
		statuses = append(statuses, status)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != StatusVerifying || statuses[1] != StatusSwitching {
		t.Fatalf("statuses = %v, want verifying then switching", statuses)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "next.db")); err != nil {
		t.Fatal(err)
	}
}
