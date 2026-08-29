package setup

import (
	"context"
	"database/sql"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

type fakeSetupDB struct {
	pingErrors  []error
	pingCalls   int
	execQueries []string
	execError   error
	closed      bool
}

func (database *fakeSetupDB) PingContext(context.Context) error {
	index := database.pingCalls
	database.pingCalls++
	if index < len(database.pingErrors) {
		return database.pingErrors[index]
	}
	return nil
}

func (database *fakeSetupDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	database.execQueries = append(database.execQueries, query)
	return fakeSetupResult(0), database.execError
}

func (database *fakeSetupDB) Close() error {
	database.closed = true
	return nil
}

type fakeSetupResult int64

func (result fakeSetupResult) LastInsertId() (int64, error) {
	return int64(result), nil
}

func (result fakeSetupResult) RowsAffected() (int64, error) {
	return int64(result), nil
}

func TestBuildMySQLDSNUsesStructuredFields(t *testing.T) {
	dsn, err := buildMySQLDSN(DatabaseSettings{
		User:     "root",
		Host:     "[2001:db8::1]",
		Port:     "03306",
		Password: "p@ss:/?#[]",
		Name:     "smail_1",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.User != "root" || parsed.Passwd != "p@ss:/?#[]" {
		t.Fatalf("unexpected credentials in DSN: user=%q password=%q", parsed.User, parsed.Passwd)
	}
	if parsed.Addr != "[2001:db8::1]:3306" {
		t.Fatalf("unexpected address: %q", parsed.Addr)
	}
	if parsed.DBName != "smail_1" || !parsed.ParseTime {
		t.Fatalf("unexpected database options: name=%q parseTime=%v", parsed.DBName, parsed.ParseTime)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("charset option is missing from DSN: %q", dsn)
	}
}

func TestBuildPostgresDSNUsesStructuredFields(t *testing.T) {
	dsn, err := buildPostgresDSN(DatabaseSettings{
		User:     "smail",
		Host:     "[2001:db8::2]",
		Port:     "05432",
		Password: "p@ss:/?#[]",
		Name:     "smail_1",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := parsePostgresURL(dsn)
	if !ok {
		t.Fatalf("invalid PostgreSQL DSN: %q", dsn)
	}
	password, hasPassword := parsed.User.Password()
	host, port := splitDatabaseAddress(parsed.Host)
	if parsed.User.Username() != "smail" || !hasPassword || password != "p@ss:/?#[]" {
		t.Fatalf("unexpected credentials in DSN: %q", dsn)
	}
	if host != "2001:db8::2" || port != "5432" || strings.TrimPrefix(parsed.Path, "/") != "smail_1" {
		t.Fatalf("unexpected connection settings: host=%q port=%q name=%q", host, port, parsed.Path)
	}
	if parsed.Query().Get("sslmode") != "disable" {
		t.Fatalf("unexpected sslmode: %q", parsed.Query().Get("sslmode"))
	}
}

func TestBuildPostgresDSNReusesMatchingPassword(t *testing.T) {
	dsn, err := buildPostgresDSN(DatabaseSettings{
		User: "smail",
		Host: "127.0.0.1",
		Port: "5432",
		Name: "smail",
	}, "postgres://smail:stored-secret@127.0.0.1:5432/smail?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := parsePostgresURL(dsn)
	if !ok {
		t.Fatalf("invalid PostgreSQL DSN: %q", dsn)
	}
	password, _ := parsed.User.Password()
	if password != "stored-secret" || parsed.Query().Get("sslmode") != "require" {
		t.Fatalf("stored PostgreSQL settings were not reused: %q", dsn)
	}
}

func TestEnsurePostgresDatabaseCreatesMissingDatabase(t *testing.T) {
	target := &fakeSetupDB{pingErrors: []error{&pq.Error{Code: "3D000"}, nil}}
	admin := &fakeSetupDB{}
	openCalls := 0
	withPostgresSetupOpener(t, func(dsn string) (setupDatabase, error) {
		openCalls++
		parsed, ok := parsePostgresURL(dsn)
		if !ok {
			return nil, stderrors.New("invalid PostgreSQL test DSN")
		}
		if strings.TrimPrefix(parsed.Path, "/") == "postgres" {
			return admin, nil
		}
		return target, nil
	})

	if err := ensurePostgresDatabase("postgres://smail:secret@127.0.0.1:5432/smail?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
	if openCalls != 2 || target.pingCalls != 2 || admin.pingCalls != 1 {
		t.Fatalf("unexpected connection flow: opens=%d targetPings=%d adminPings=%d", openCalls, target.pingCalls, admin.pingCalls)
	}
	if len(admin.execQueries) != 1 || admin.execQueries[0] != `CREATE DATABASE "smail"` {
		t.Fatalf("unexpected creation query: %#v", admin.execQueries)
	}
}

func withPostgresSetupOpener(t *testing.T, opener func(string) (setupDatabase, error)) {
	t.Helper()
	original := openPostgresSetupDatabase
	openPostgresSetupDatabase = opener
	t.Cleanup(func() {
		openPostgresSetupDatabase = original
	})
}

func TestBuildMySQLDSNAppliesDefaults(t *testing.T) {
	dsn, err := buildMySQLDSN(DatabaseSettings{User: "root"}, "")
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Addr != "127.0.0.1:3306" || parsed.DBName != "smail" {
		t.Fatalf("unexpected defaults: address=%q database=%q", parsed.Addr, parsed.DBName)
	}
}

func TestBuildMySQLDSNReusesMatchingPassword(t *testing.T) {
	previous := mysql.NewConfig()
	previous.User = "root"
	previous.Passwd = "stored-secret"
	previous.Net = "tcp"
	previous.Addr = "[::1]:3306"
	previous.DBName = "smail"

	dsn, err := buildMySQLDSN(DatabaseSettings{
		User: "root",
		Host: "::1",
		Port: "03306",
		Name: "smail",
	}, previous.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Passwd != "stored-secret" {
		t.Fatal("matching stored password was not reused")
	}
}

func TestBuildMySQLDSNRejectsInvalidInput(t *testing.T) {
	tests := []DatabaseSettings{
		{User: "root", Port: "0", Name: "smail"},
		{User: "root", Port: "65536", Name: "smail"},
		{User: "root", Port: "3306", Name: "smail-test"},
		{Port: "3306", Name: "smail"},
	}
	for _, settings := range tests {
		if _, err := buildMySQLDSN(settings, ""); err == nil {
			t.Fatalf("invalid settings were accepted: %+v", settings)
		}
	}
}

func TestEnsureMySQLDatabaseSkipsCreationWhenDatabaseExists(t *testing.T) {
	target := &fakeSetupDB{}
	openCalls := 0
	withMySQLSetupOpener(t, func(string) (setupDatabase, error) {
		openCalls++
		return target, nil
	})

	if err := ensureMySQLDatabase(testMySQLDSN(t, "secret")); err != nil {
		t.Fatal(err)
	}
	if openCalls != 1 {
		t.Fatalf("unexpected open count: %d", openCalls)
	}
	if len(target.execQueries) != 0 || !target.closed {
		t.Fatal("existing database should not trigger creation and must be closed")
	}
}

func TestEnsureMySQLDatabaseCreatesMissingDatabase(t *testing.T) {
	target := &fakeSetupDB{pingErrors: []error{&mysql.MySQLError{Number: 1049}, nil}}
	admin := &fakeSetupDB{}
	openCalls := 0
	withMySQLSetupOpener(t, func(dsn string) (setupDatabase, error) {
		openCalls++
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, err
		}
		if parsed.DBName == "" {
			return admin, nil
		}
		return target, nil
	})

	if err := ensureMySQLDatabase(testMySQLDSN(t, "secret")); err != nil {
		t.Fatal(err)
	}
	if openCalls != 2 || target.pingCalls != 2 || admin.pingCalls != 1 {
		t.Fatalf("unexpected connection flow: opens=%d targetPings=%d adminPings=%d", openCalls, target.pingCalls, admin.pingCalls)
	}
	if len(admin.execQueries) != 1 || admin.execQueries[0] != "CREATE DATABASE IF NOT EXISTS `smail` DEFAULT CHARACTER SET utf8mb4" {
		t.Fatalf("unexpected creation query: %#v", admin.execQueries)
	}
	if !target.closed || !admin.closed {
		t.Fatal("setup database connections were not closed")
	}
}

func TestEnsureMySQLDatabaseReturnsSafeCreationError(t *testing.T) {
	target := &fakeSetupDB{pingErrors: []error{&mysql.MySQLError{Number: 1049}}}
	admin := &fakeSetupDB{execError: stderrors.New("secret-password")}
	withMySQLSetupOpener(t, func(dsn string) (setupDatabase, error) {
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, err
		}
		if parsed.DBName == "" {
			return admin, nil
		}
		return target, nil
	})

	err := ensureMySQLDatabase(testMySQLDSN(t, "secret-password"))
	if err == nil {
		t.Fatal("database creation error was not returned")
	}
	message := DatabaseSetupErrorMessage(err)
	if message == "" || strings.Contains(message, "secret-password") {
		t.Fatalf("unsafe user-facing error message: %q", message)
	}
}

func withMySQLSetupOpener(t *testing.T, opener func(string) (setupDatabase, error)) {
	t.Helper()
	original := openMySQLSetupDatabase
	openMySQLSetupDatabase = opener
	t.Cleanup(func() {
		openMySQLSetupDatabase = original
	})
}

func testMySQLDSN(t *testing.T, password string) string {
	t.Helper()
	dsn, err := buildMySQLDSN(DatabaseSettings{
		User:     "root",
		Host:     "127.0.0.1",
		Port:     "3306",
		Password: password,
		Name:     "smail",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	return dsn
}

func TestPrepareSQLiteDatabaseCreatesParentDirectory(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "nested", "config", "smail.db")
	if err := prepareSQLiteDatabase(databasePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(databasePath)); err != nil {
		t.Fatalf("SQLite parent directory was not prepared: %v", err)
	}
}
