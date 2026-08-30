package migration

import (
	"context"
	"database/sql"
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
	wantVersion := int64(len(DefaultMigrations()))
	if report.CurrentVersion != wantVersion || len(report.Applied) != int(wantVersion) {
		t.Fatalf("report = %+v", report)
	}

	for _, table := range []string{"user", "email", "group", "rule", "sessions", "user_email", "version", "external_email", "verification_event", "security_question", "recovery_code", "verification_rate_limit", "verification_cleanup_state", "webauthn_credential", "totp_authenticator", "plugin_secret", "plugin_user_setting", "schema_migration"} {
		if !sqliteTableExists(t, db, table) {
			t.Fatalf("table %s was not created", table)
		}
	}
	for _, column := range []string{"role", "session_version", "security_version", "must_change_password", "password_changed_at"} {
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
	for _, column := range []string{"address", "address_canonical", "is_primary", "verified_at", "created_at", "updated_at"} {
		exists, err := columnExists(context.Background(), db, DialectSQLite, "user_email", column)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("column user_email.%s was not created", column)
		}
	}
	for _, index := range []string{"uq_user_email_address_canonical", "idx_user_email_user_primary"} {
		if !sqliteIndexExists(t, db, index) {
			t.Fatalf("index %s was not created", index)
		}
	}
}

func sqliteIndexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
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
	var securityVersion int
	var mustChangePassword int
	var passwordChangedAt any
	if err := db.QueryRow(`SELECT "role", "session_version", "security_version", "must_change_password", "password_changed_at" FROM "user" WHERE "account" = 'root'`).Scan(&role, &sessionVersion, &securityVersion, &mustChangePassword, &passwordChangedAt); err != nil {
		t.Fatal(err)
	}
	if role != "super_admin" || sessionVersion != 1 || securityVersion != 1 || mustChangePassword != 0 || passwordChangedAt != nil {
		t.Fatalf("role = %q, session version = %d, security version = %d, must change password = %d, password changed at = %v", role, sessionVersion, securityVersion, mustChangePassword, passwordChangedAt)
	}
}

func TestUserEmailLoginAddressMigrationUpgradesLegacyTableAndIsIdempotent(t *testing.T) {
	db := openSQLite(t)
	if _, err := db.Exec(`CREATE TABLE "user_email" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email_id" INTEGER NOT NULL, "is_read" INTEGER NOT NULL DEFAULT 0, "group_id" INTEGER NOT NULL DEFAULT 0, "status" INTEGER NOT NULL DEFAULT 0, "create" DATETIME NULL)`); err != nil {
		t.Fatal(err)
	}

	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: DefaultMigrations()}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"address", "address_canonical", "is_primary", "verified_at", "created_at", "updated_at"} {
		exists, err := columnExists(context.Background(), db, DialectSQLite, "user_email", column)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("legacy table is missing migrated column %s", column)
		}
	}
	for _, index := range []string{"uq_user_email_address_canonical", "idx_user_email_user_primary"} {
		if !sqliteIndexExists(t, db, index) {
			t.Fatalf("legacy table is missing migrated index %s", index)
		}
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) != 0 || report.CurrentVersion != int64(len(DefaultMigrations())) {
		t.Fatalf("repeated migration report = %+v", report)
	}
}

func TestUserEmailLoginAddressMigrationRejectsDuplicateCanonicalAddresses(t *testing.T) {
	db := openSQLite(t)
	if _, err := db.Exec(`CREATE TABLE "user_email" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email_id" INTEGER NOT NULL, "address" TEXT NULL, "address_canonical" TEXT NULL, "is_primary" INTEGER NOT NULL DEFAULT 0, "verified_at" DATETIME NULL, "created_at" DATETIME NULL, "updated_at" DATETIME NULL, "is_read" INTEGER NOT NULL DEFAULT 0, "group_id" INTEGER NOT NULL DEFAULT 0, "status" INTEGER NOT NULL DEFAULT 0, "create" DATETIME NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "user_email"("user_id", "email_id", "address", "address_canonical") VALUES (1, 1, 'one@example.com', 'duplicate@example.com'), (2, 2, 'two@example.com', 'duplicate@example.com')`); err != nil {
		t.Fatal(err)
	}

	err := applyUserEmailAddressSchema(context.Background(), db, DialectSQLite)
	if err == nil || !strings.Contains(err.Error(), "conflict count: 1") {
		t.Fatalf("duplicate canonical address error = %v", err)
	}
	if sqliteIndexExists(t, db, "uq_user_email_address_canonical") {
		t.Fatal("unique index was created despite duplicate canonical addresses")
	}
}

func TestUserEmailLoginAddressMigrationRejectsUnexpectedDialect(t *testing.T) {
	err := applyUserEmailAddressSchema(context.Background(), nil, Dialect("unsupported"))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected dialect error = %v", err)
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
