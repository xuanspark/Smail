package dbmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

func portableFixture(t *testing.T) (string, config.DatabaseConfig) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(root, "data", "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := (migration.Runner{DB: database, Dialect: migration.DialectSQLite, Migrations: migration.DefaultMigrations()}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO "user" (id,account,name,password) VALUES (40,'review','测试','hash')`,
		`INSERT INTO email (id,subject,text,create_time) VALUES (82,'中文主题','正文😀','2026-09-05 12:30:00.123456')`,
		`INSERT INTO sessions (token,data,expiry) VALUES ('token',x'0001ff',julianday('2026-09-12 12:00:00'))`,
		`INSERT INTO plugin_user_setting (plugin_id,user_id,schema_version,settings_ciphertext,settings_hash,enabled,created_at,updated_at) VALUES ('test',40,1,'cipher','hash',1,'2026-09-05 12:00:00','2026-09-05 12:00:00')`,
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return root, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "source.db"}
}

func TestPortableSQLiteCopyPreservesDataAndLocksUntilRestart(t *testing.T) {
	root, source := portableFixture(t)
	var switched bool
	executor := &PortableExecutor{root: root, switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		switched = true
		return nil
	}}
	plan, err := buildTargetPlan(source, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "target.db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Preview(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "target.db")); !os.IsNotExist(err) {
		t.Fatal("preview created target")
	}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); err != nil {
		t.Fatal(err)
	}
	defer executor.releaseSource()
	if !switched || !executor.pending {
		t.Fatal("switch or restart state missing")
	}
	target, err := executor.connect(context.Background(), plan.targetConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var subject string
	if err := target.QueryRow("SELECT subject FROM email WHERE id=82").Scan(&subject); err != nil || subject != "中文主题" {
		t.Fatal("content not preserved")
	}
	var expiry float64
	if err := target.QueryRow("SELECT expiry FROM sessions").Scan(&expiry); err != nil || expiry < 2400000 || expiry > 2500000 {
		t.Fatal("session Julian date not preserved")
	}
	if _, err := target.Exec(`INSERT INTO "user" (account,name,password) VALUES ('next','新增','hash')`); err != nil {
		t.Fatal(err)
	}
	var next int
	if err := target.QueryRow(`SELECT id FROM "user" WHERE account='next'`).Scan(&next); err != nil || next <= 40 {
		t.Fatal("identity sequence not advanced")
	}
	old, err := sql.Open("sqlite", filepath.Join(root, "data", "source.db")+"?_pragma=busy_timeout(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := old.Exec(`UPDATE "user" SET name='bad'`); err == nil {
		t.Fatal("old source accepted writes after switch")
	}
}

func TestPortableSwitchFailurePreservesSourceAndTarget(t *testing.T) {
	root, source := portableFixture(t)
	executor := &PortableExecutor{root: root, switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		return config.ErrGenerationConflict
	}}
	plan, err := buildTargetPlan(source, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "target.db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); !errors.Is(err, config.ErrGenerationConflict) {
		t.Fatalf("switch failure: %v", err)
	}
	if executor.pending {
		t.Fatal("rejected switch kept write lock")
	}
	for _, name := range []string{"source.db", "target.db"} {
		if _, err := os.Stat(filepath.Join(root, "data", name)); err != nil {
			t.Fatal("database removed")
		}
	}
	if err := executor.Preview(context.Background(), plan); !errors.Is(err, ErrTargetExists) {
		t.Fatal("existing target accepted")
	}
}

func TestTargetSupportsAllDatabasePairsWithoutSecretDisclosure(t *testing.T) {
	root := t.TempDir()
	inputs := []TargetInput{
		{Type: config.DatabaseTypeSQLite, LocalRef: "next.db"},
		{Type: config.DatabaseTypeMySQL, Host: "localhost", Username: "test", Name: "next", Password: &config.SecretInput{Plaintext: "private-test-value"}},
		{Type: config.DatabaseTypePostgreSQL, Host: "localhost", Username: "test", Name: "next", Password: &config.SecretInput{Plaintext: "private-test-value"}},
	}
	for _, input := range inputs {
		target, err := input.Prepare(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, sourceInput := range inputs {
			sourceInput.Name = ""
			if sourceInput.Type == config.DatabaseTypeSQLite {
				sourceInput.LocalRef = "source.db"
			} else {
				sourceInput.Name = "source"
			}
			source, err := sourceInput.Prepare(root)
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(testRunner{})
			record, _, err := service.PreviewTarget(context.Background(), source, 1, target)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(record)
			if strings.Contains(string(data), "password") || strings.Contains(string(data), "private-test-value") || strings.Contains(string(data), "ciphertext") {
				t.Fatal("preview leaked password")
			}
		}
		if target.Type != config.DatabaseTypeSQLite {
			vault, _ := credential.NewVault(root)
			value, err := config.OpenStoredSecret(vault, target.Password)
			if err != nil || value != "private-test-value" {
				t.Fatal("target secret did not roundtrip")
			}
		}
	}
}

func TestPortableConversions(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	julian, err := convertPortableValue(now, "sessions", "expiry", "REAL", config.DatabaseTypeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := convertPortableValue(julian, "sessions", "expiry", "TIMESTAMPTZ", config.DatabaseTypePostgreSQL)
	if err != nil || restored.(time.Time).Sub(now).Abs() > time.Millisecond {
		t.Fatal("session expiry did not roundtrip")
	}
	boolean, err := convertPortableValue(int64(1), "plugin_user_setting", "enabled", "BOOL", config.DatabaseTypePostgreSQL)
	if err != nil || boolean != true {
		t.Fatal("boolean conversion failed")
	}
	if _, err := convertPortableValue(int64(2), "plugin_user_setting", "enabled", "BOOL", config.DatabaseTypePostgreSQL); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}

func TestPortableRejectsCustomObjectsBeforeCreatingTarget(t *testing.T) {
	root, source := portableFixture(t)
	executor := &PortableExecutor{root: root, switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
		t.Error("unsupported schema switched configuration")
		return nil
	}}
	database, err := executor.connect(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE VIEW custom_view AS SELECT id FROM "user"`); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	plan, err := buildTargetPlan(source, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "target.db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); !errors.Is(err, ErrSQLUnsupportedObject) {
		t.Fatalf("custom view accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "target.db")); !os.IsNotExist(err) {
		t.Fatal("unsupported source created target")
	}
}
