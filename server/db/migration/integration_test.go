package migration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

const integrationTimeout = 45 * time.Second

type integrationDatabaseFactory func(*testing.T) (*sql.DB, func())

func TestMySQLIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SMAIL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("SMAIL_TEST_MYSQL_DSN is not set")
	}
	runIntegrationSuite(t, DialectMySQL, mysqlIntegrationDatabase(dsn))
}

func TestPostgresIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SMAIL_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("SMAIL_TEST_POSTGRES_DSN is not set")
	}
	runIntegrationSuite(t, DialectPostgres, postgresIntegrationDatabase(dsn))
}

func runIntegrationSuite(t *testing.T, dialect Dialect, factory integrationDatabaseFactory) {
	t.Helper()
	t.Run("first_and_repeated_run", func(t *testing.T) {
		db, cleanup := factory(t)
		defer cleanup()
		runner := Runner{DB: db, Dialect: dialect, Migrations: DefaultMigrations(), LockWait: 10 * time.Second}
		first := runIntegrationMigrations(t, runner)
		if first.CurrentVersion != 10 || len(first.Applied) != 10 {
			t.Fatalf("first report = %+v", first)
		}
		second := runIntegrationMigrations(t, runner)
		if second.CurrentVersion != 10 || len(second.Applied) != 0 {
			t.Fatalf("second report = %+v", second)
		}
	})

	t.Run("concurrent_run", func(t *testing.T) {
		db, cleanup := factory(t)
		defer cleanup()
		db.SetMaxOpenConns(4)
		runner := Runner{DB: db, Dialect: dialect, Migrations: DefaultMigrations(), LockWait: 15 * time.Second}
		ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		var wg sync.WaitGroup
		reports := make(chan Report, 2)
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				report, err := runner.Run(ctx)
				if err != nil {
					errs <- err
					return
				}
				reports <- report
			}()
		}
		wg.Wait()
		close(reports)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		applied := 0
		for report := range reports {
			applied += len(report.Applied)
		}
		if applied != 10 {
			t.Fatalf("applied migration count = %d, want 10", applied)
		}
		assertIntegrationCount(t, db, "schema_migration", 10)
	})

	t.Run("failed_migration_recovery", func(t *testing.T) {
		db, cleanup := factory(t)
		defer cleanup()
		if _, err := db.Exec("CREATE TABLE recovery_probe (id BIGINT PRIMARY KEY, value VARCHAR(64) NOT NULL)"); err != nil {
			t.Fatal(err)
		}
		broken := Runner{
			DB:       db,
			Dialect:  dialect,
			LockWait: 10 * time.Second,
			Migrations: []Migration{
				Define(1, "recoverable", "INSERT INTO recovery_probe(id, value) VALUES (1, 'pending')", "INSERT INTO missing_recovery_probe(id) VALUES (1)"),
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		_, err := broken.Run(ctx)
		cancel()
		if err == nil {
			t.Fatal("expected migration failure")
		}
		assertIntegrationCount(t, db, "recovery_probe", 0)
		assertIntegrationCount(t, db, "schema_migration", 0)

		fixed := Runner{
			DB:         db,
			Dialect:    dialect,
			LockWait:   10 * time.Second,
			Migrations: []Migration{Define(1, "recoverable", "INSERT INTO recovery_probe(id, value) VALUES (1, 'recovered')")},
		}
		report := runIntegrationMigrations(t, fixed)
		if report.CurrentVersion != 1 || len(report.Applied) != 1 {
			t.Fatalf("recovery report = %+v", report)
		}
		assertIntegrationCount(t, db, "recovery_probe", 1)
		assertIntegrationCount(t, db, "schema_migration", 1)
	})
}

func runIntegrationMigrations(t *testing.T, runner Runner) Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	report, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func assertIntegrationCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}

func mysqlIntegrationDatabase(dsn string) integrationDatabaseFactory {
	return func(t *testing.T) (*sql.DB, func()) {
		t.Helper()
		config, err := mysqlDriver.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		admin, err := sql.Open("mysql", config.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		if err = admin.PingContext(ctx); err != nil {
			_ = admin.Close()
			t.Fatal(err)
		}
		name := integrationIdentifier("smail_migration_test")
		if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4"); err != nil {
			_ = admin.Close()
			t.Fatal(err)
		}
		testConfig := *config
		testConfig.DBName = name
		testConfig.ParseTime = true
		db, err := sql.Open("mysql", testConfig.FormatDSN())
		if err != nil {
			_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
			_ = admin.Close()
			t.Fatal(err)
		}
		if err = db.PingContext(ctx); err != nil {
			_ = db.Close()
			_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
			_ = admin.Close()
			t.Fatal(err)
		}
		cleanup := func() {
			_ = db.Close()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), integrationTimeout)
			defer cleanupCancel()
			_, _ = admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+name+"`")
			_ = admin.Close()
		}
		return db, cleanup
	}
}

func postgresIntegrationDatabase(dsn string) integrationDatabaseFactory {
	return func(t *testing.T) (*sql.DB, func()) {
		t.Helper()
		admin, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		if err = admin.PingContext(ctx); err != nil {
			_ = admin.Close()
			t.Fatal(err)
		}
		name := integrationIdentifier("smail_migration_test")
		if _, err = admin.ExecContext(ctx, "CREATE SCHEMA \""+name+"\""); err != nil {
			_ = admin.Close()
			t.Fatal(err)
		}
		testDSN, err := postgresDSNWithSchema(dsn, name)
		if err != nil {
			_, _ = admin.Exec("DROP SCHEMA IF EXISTS \"" + name + "\" CASCADE")
			_ = admin.Close()
			t.Fatal(err)
		}
		db, err := sql.Open("postgres", testDSN)
		if err != nil {
			_, _ = admin.Exec("DROP SCHEMA IF EXISTS \"" + name + "\" CASCADE")
			_ = admin.Close()
			t.Fatal(err)
		}
		if err = db.PingContext(ctx); err != nil {
			_ = db.Close()
			_, _ = admin.Exec("DROP SCHEMA IF EXISTS \"" + name + "\" CASCADE")
			_ = admin.Close()
			t.Fatal(err)
		}
		cleanup := func() {
			_ = db.Close()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), integrationTimeout)
			defer cleanupCancel()
			_, _ = admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS \""+name+"\" CASCADE")
			_ = admin.Close()
		}
		return db, cleanup
	}
}

func postgresDSNWithSchema(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema, nil
}

func integrationIdentifier(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
