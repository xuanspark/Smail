package migration

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultMigrationsCreateCompleteSQLiteSchema(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: DefaultMigrations()}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentVersion != 9 || len(report.Applied) != 9 {
		t.Fatalf("report = %+v", report)
	}

	for _, table := range []string{"user", "email", "group", "rule", "sessions", "user_email", "version", "external_email", "verification_event", "security_question", "recovery_code", "verification_rate_limit", "verification_cleanup_state", "webauthn_credential", "totp_authenticator", "schema_migration"} {
		if !sqliteTableExists(t, db, table) {
			t.Fatalf("table %s was not created", table)
		}
	}
	for _, column := range []string{"role", "session_version"} {
		exists, err := columnExists(context.Background(), db, DialectSQLite, "user", column)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("column user.%s was not created", column)
		}
	}
	exists, err := columnExists(context.Background(), db, DialectSQLite, "webauthn_credential", "discoverable")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("column webauthn_credential.discoverable was not created")
	}
}

func TestDefaultMigrationsUpgradeLegacyUserSchema(t *testing.T) {
	db := openSQLite(t)
	if _, err := db.Exec(`CREATE TABLE "user" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "account" TEXT NOT NULL UNIQUE, "name" TEXT NOT NULL, "password" CHAR(32) NOT NULL, "disabled" INTEGER NOT NULL DEFAULT 0, "is_admin" INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "user"("account", "name", "password", "is_admin") VALUES ('root', 'root', 'legacy', 1)`); err != nil {
		t.Fatal(err)
	}

	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: DefaultMigrations()}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var role string
	var sessionVersion int
	if err := db.QueryRow(`SELECT "role", "session_version" FROM "user" WHERE "account" = 'root'`).Scan(&role, &sessionVersion); err != nil {
		t.Fatal(err)
	}
	if role != "super_admin" || sessionVersion != 1 {
		t.Fatalf("role = %q, session version = %d", role, sessionVersion)
	}
}

func TestSecurityEmailMySQLSchemaDoesNotCreateDuplicateIndex(t *testing.T) {
	statements := securityEmailStatements()[DialectMySQL]
	for _, statement := range statements {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE INDEX IDX_EXTERNAL_EMAIL_USER_ID") {
			t.Fatal("external email index is created twice")
		}
	}
}

func TestCustomMigrationChecksumIncludesSeed(t *testing.T) {
	first := DefineCustom(1, "custom", "seed-a", func(context.Context, executor, Dialect) error { return nil })
	second := DefineCustom(1, "custom", "seed-b", func(context.Context, executor, Dialect) error { return nil })
	if first.Checksum() == second.Checksum() {
		t.Fatal("custom migration checksum ignored its seed")
	}
}

func TestDefaultMigrationsAreContiguousAndDialectComplete(t *testing.T) {
	migrations := DefaultMigrations()
	if len(migrations) == 0 {
		t.Fatal("default migrations are empty")
	}
	for index, item := range migrations {
		wantVersion := int64(index + 1)
		if item.Version != wantVersion {
			t.Fatalf("migration %d has version %d, want %d", index, item.Version, wantVersion)
		}
		if item.Name == "" {
			t.Fatalf("migration %d has an empty name", item.Version)
		}
		for _, dialect := range []Dialect{DialectSQLite, DialectMySQL, DialectPostgres} {
			if item.Apply == nil && len(item.StatementsFor(dialect)) == 0 {
				t.Fatalf("migration %d (%s) has no statements for %s", item.Version, item.Name, dialect)
			}
		}
	}
	if _, err := normalize(migrations); err != nil {
		t.Fatalf("default migrations failed normalization: %v", err)
	}
}
