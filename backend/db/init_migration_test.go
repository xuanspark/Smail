package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"xorm.io/xorm/convert"
)

func TestDatabaseEnginePreservesMigratedUTCTime(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	t.Cleanup(func() { time.Local = previousLocal })

	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{"sqlite", ":memory:"},
		{"mysql", "smail@tcp(127.0.0.1:3306)/smail?parseTime=true"},
		{"postgres", "host=127.0.0.1 user=smail dbname=smail sslmode=require"},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			engine, err := newDatabaseEngine(tc.driver, tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			if engine.GetTZDatabase() != time.UTC {
				t.Fatalf("database timezone = %v, want UTC", engine.GetTZDatabase())
			}
			if engine.GetTZLocation() != time.Local {
				t.Fatalf("display timezone = %v, want CST", engine.GetTZLocation())
			}
			want := time.Date(2026, 10, 3, 16, 56, 0, 0, time.UTC)
			for _, value := range []any{want, "2026-10-03 16:56:00"} {
				got, err := convert.AsTime(value, engine.GetTZDatabase(), engine.GetTZLocation())
				if err != nil {
					t.Fatal(err)
				}
				if got == nil || !got.Equal(want) {
					t.Fatalf("converted time = %v, want instant %v", got, want)
				}
				if display := got.Format("2006-01-02 15:04:05"); display != "2026-10-04 00:56:00" {
					t.Fatalf("display time = %s, want 2026-10-04 00:56:00", display)
				}
			}
		})
	}
}

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
