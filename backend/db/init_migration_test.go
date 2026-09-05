package db

import (
	"context"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
)

func TestInitAppliesSchemaMigration(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "init.db")
	previousConfig := config.Instance
	previousInstance := Instance
	t.Cleanup(func() {
		if Instance != nil && Instance != previousInstance {
			_ = Instance.Close()
		}
		Instance = previousInstance
		config.Instance = previousConfig
	})

	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: databasePath}
	if err := Init("test"); err != nil {
		t.Fatal(err)
	}
	runner := migration.Runner{DB: Instance.DB().DB, Dialect: migration.DialectSQLite, Migrations: migration.DefaultMigrations()}
	wantVersion := int64(len(migration.DefaultMigrations()))
	version, err := runner.CurrentVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != wantVersion {
		t.Fatalf("schema version = %d, want %d", version, wantVersion)
	}
	var count int
	if _, err = Instance.SQL("SELECT count(*) FROM schema_migration").Get(&count); err != nil {
		t.Fatal(err)
	}
	if count != int(wantVersion) {
		t.Fatalf("schema migration rows = %d, want %d", count, wantVersion)
	}
}

func TestLegacySyncRequiresExplicitDevelopmentOrTestMode(t *testing.T) {
	previousDevelopment := config.DevelopmentMode
	t.Cleanup(func() { config.DevelopmentMode = previousDevelopment })

	t.Setenv("SMAIL_ENABLE_LEGACY_SYNC2", "")
	config.DevelopmentMode = true
	if legacySyncEnabled("test") {
		t.Fatal("legacy Sync2 was enabled without the compatibility flag")
	}

	t.Setenv("SMAIL_ENABLE_LEGACY_SYNC2", "1")
	config.DevelopmentMode = false
	if legacySyncEnabled("release") {
		t.Fatal("legacy Sync2 was enabled in production")
	}
	if !legacySyncEnabled("test") {
		t.Fatal("legacy Sync2 was not enabled for an explicit test run")
	}

	config.DevelopmentMode = true
	if !legacySyncEnabled("release") {
		t.Fatal("legacy Sync2 was not enabled in development mode")
	}
}
