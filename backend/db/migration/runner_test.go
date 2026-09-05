package migration

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteFirstAndRepeatedRun(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: testMigrations()}

	first, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.CurrentVersion != 2 || len(first.Applied) != 2 {
		t.Fatalf("first report = %+v", first)
	}
	second, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.CurrentVersion != 2 || len(second.Applied) != 0 {
		t.Fatalf("second report = %+v", second)
	}
	version, err := runner.CurrentVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("version = %d, want 2", version)
	}
	assertRowCount(t, db, "sample", 1)
}

func TestSQLiteChecksumConflict(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: testMigrations()}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	changed := testMigrations()
	changed[0].Statements[0] = "CREATE TABLE sample (id INTEGER PRIMARY KEY, changed TEXT)"
	_, err := (Runner{DB: db, Dialect: DialectSQLite, Migrations: changed}).Run(context.Background())
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}

func TestSQLiteCompatibleChecksumAccepted(t *testing.T) {
	db := openSQLite(t)
	migrations := testMigrations()
	if _, err := (Runner{DB: db, Dialect: DialectSQLite, Migrations: migrations}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	const compatibleChecksum = "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := db.Exec("UPDATE schema_migration SET checksum = ? WHERE version = 1", compatibleChecksum); err != nil {
		t.Fatal(err)
	}
	migrations[0] = WithCompatibleChecksums(migrations[0], compatibleChecksum)
	report, err := (Runner{DB: db, Dialect: DialectSQLite, Migrations: migrations}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentVersion != 2 || len(report.Applied) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestSQLiteCompatibleChecksumStillRequiresMatchingName(t *testing.T) {
	db := openSQLite(t)
	migrations := testMigrations()
	if _, err := (Runner{DB: db, Dialect: DialectSQLite, Migrations: migrations}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	const compatibleChecksum = "2222222222222222222222222222222222222222222222222222222222222222"
	if _, err := db.Exec("UPDATE schema_migration SET name = 'changed', checksum = ? WHERE version = 1", compatibleChecksum); err != nil {
		t.Fatal(err)
	}
	migrations[0] = WithCompatibleChecksums(migrations[0], compatibleChecksum)
	_, err := (Runner{DB: db, Dialect: DialectSQLite, Migrations: migrations}).Run(context.Background())
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}
func TestSQLiteFailedMigrationRollsBack(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{
		DB:      db,
		Dialect: DialectSQLite,
		Migrations: []Migration{
			Define(1, "broken", "CREATE TABLE should_rollback (id INTEGER PRIMARY KEY)", "INVALID SQL"),
		},
	}
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("expected migration failure")
	}
	if sqliteTableExists(t, db, "should_rollback") {
		t.Fatal("failed migration left a partial table")
	}
	if sqliteTableExists(t, db, "schema_migration") {
		assertRowCount(t, db, "schema_migration", 0)
	}
}

func TestSQLiteConcurrentRunHasSingleExecutor(t *testing.T) {
	db := openSQLite(t)
	db.SetMaxOpenConns(4)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: testMigrations()}

	var wg sync.WaitGroup
	reports := make(chan Report, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := runner.Run(context.Background())
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
	appliedCount := 0
	for report := range reports {
		appliedCount += len(report.Applied)
	}
	if appliedCount != 2 {
		t.Fatalf("applied migration count = %d, want 2", appliedCount)
	}
	assertRowCount(t, db, "schema_migration", 2)
	assertRowCount(t, db, "sample", 1)
}

func TestUnknownAppliedMigrationIsRejected(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: testMigrations()}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migration(version, name, checksum, applied_at, execution_ms) VALUES (99, 'future', 'x', CURRENT_TIMESTAMP, 0)"); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Run(context.Background())
	if !errors.Is(err, ErrUnknownAppliedMigration) {
		t.Fatalf("error = %v, want unknown migration", err)
	}
}

func TestNormalizeRejectsInvalidDefinitions(t *testing.T) {
	tests := [][]Migration{
		{Define(0, "invalid", "SELECT 1")},
		{Define(1, "", "SELECT 1")},
		{Define(1, "empty")},
		{Define(1, "one", "SELECT 1"), Define(1, "duplicate", "SELECT 1")},
		{Define(2, "missing_first", "SELECT 1")},
		{Define(1, "one", "SELECT 1"), Define(3, "gap", "SELECT 1")},
	}
	for i, migrations := range tests {
		if _, err := normalize(migrations); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestNormalizeRejectsInvalidCompatibleChecksums(t *testing.T) {
	base := Define(1, "create_sample", "CREATE TABLE sample (id INTEGER PRIMARY KEY)")
	migrations := [][]Migration{
		{WithCompatibleChecksums(base, "invalid")},
		{WithCompatibleChecksums(base, base.Checksum())},
	}
	for index, items := range migrations {
		if _, err := normalize(items); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}
func testMigrations() []Migration {
	return []Migration{
		Define(1, "create_sample", "CREATE TABLE sample (id INTEGER PRIMARY KEY, value TEXT NOT NULL)"),
		Define(2, "seed_sample", "INSERT INTO sample(id, value) VALUES (1, 'ok')"),
	}
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "migration.db") + "?_pragma=busy_timeout(1000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertRowCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}

func sqliteTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func TestRunnerRejectsMissingDialectStatements(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{
		DB:      db,
		Dialect: DialectSQLite,
		Migrations: []Migration{
			DefineForDialects(1, "mysql_only", map[Dialect][]string{DialectMySQL: {"SELECT 1"}}),
		},
	}
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("migration without SQLite statements was accepted")
	}
}
