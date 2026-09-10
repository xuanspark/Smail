package migration

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
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
	for _, column := range []string{"role", "session_version", "security_version", "must_change_password", "password_changed_at", "security_setup_acknowledged", "external_email_enabled", "webauthn_enabled"} {
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
	for _, column := range []string{"address", "address_canonical", "is_primary", "verified_at", "created_at", "updated_at", "is_deleted"} {
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
	exists, err = columnExists(context.Background(), db, DialectSQLite, "operation_log", "actor_role")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("column operation_log.actor_role was not created")
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
	var externalEmailEnabled int
	var webAuthnEnabled int
	if err := db.QueryRow(`SELECT "role", "session_version", "security_version", "must_change_password", "password_changed_at", "external_email_enabled", "webauthn_enabled" FROM "user" WHERE "account" = 'root'`).Scan(&role, &sessionVersion, &securityVersion, &mustChangePassword, &passwordChangedAt, &externalEmailEnabled, &webAuthnEnabled); err != nil {
		t.Fatal(err)
	}
	if role != "super_admin" || sessionVersion != 1 || securityVersion != 1 || mustChangePassword != 0 || passwordChangedAt != nil || externalEmailEnabled != 1 || webAuthnEnabled != 1 {
		t.Fatalf("role = %q, session version = %d, security version = %d, must change password = %d, password changed at = %v, external email enabled = %d, webauthn enabled = %d", role, sessionVersion, securityVersion, mustChangePassword, passwordChangedAt, externalEmailEnabled, webAuthnEnabled)
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
	for _, column := range []string{"address", "address_canonical", "is_primary", "verified_at", "created_at", "updated_at", "is_deleted"} {
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

func TestIMAPDeletedFlagMigrationRejectsUnexpectedDialect(t *testing.T) {
	err := applyIMAPDeletedFlagSchema(context.Background(), nil, Dialect("unsupported"))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected dialect error = %v", err)
	}
}

func TestUserEmailLoginAddressMigrationRejectsUnexpectedDialect(t *testing.T) {
	err := applyUserEmailAddressSchema(context.Background(), nil, Dialect("unsupported"))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected dialect error = %v", err)
	}
}

func TestOperationLogActorRoleMigrationBackfillsValidatedRoles(t *testing.T) {
	db := openSQLite(t)
	for _, statement := range logStatements()[DialectSQLite] {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct {
		id      string
		payload string
		want    any
	}{
		{id: "user", payload: `{"actorRole":"user"}`, want: "user"},
		{id: "admin", payload: `{"actorRole":" admin "}`, want: "admin"},
		{id: "super", payload: `{"actorRole":"super_admin"}`, want: "super_admin"},
		{id: "missing", payload: `{}`, want: nil},
		{id: "unknown", payload: `{"actorRole":"owner"}`, want: nil},
		{id: "invalid", payload: `{`, want: nil},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO operation_log (id, created_at, kind, actor_id, action, section, result, payload) VALUES (?, 1, 'operation', '1', 'test', '', 'success', ?)`, row.id, row.payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyOperationLogActorRoleSchema(context.Background(), db, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if err := applyOperationLogActorRoleSchema(context.Background(), db, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var got sql.NullString
		if err := db.QueryRow(`SELECT actor_role FROM operation_log WHERE id = ?`, row.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if row.want == nil {
			if got.Valid {
				t.Fatalf("invalid historical role %s was backfilled as %q", row.id, got.String)
			}
			continue
		}
		if !got.Valid || got.String != row.want {
			t.Fatalf("historical role %s = %q, want %q", row.id, got.String, row.want)
		}
	}
}

func TestOperationLogActorRoleMigrationRejectsUnexpectedDialect(t *testing.T) {
	err := applyOperationLogActorRoleSchema(context.Background(), nil, Dialect("unsupported"))
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

func TestDefaultMigrationChecksumsAreImmutable(t *testing.T) {
	expected := map[int64]string{
		1:  "bb9bbf1ef17f1430900b8c5bed3539d01f72d49bdfedc2528dd3cf089b2a00d8",
		2:  "b91524dc66ba86962372368ed7254e0c33eda6f76d487d6d3d291574cbf41345",
		3:  "675251af1f5cd5d03f645c4b4ac15db2b488f8a3c9a972e836a136450da67c52",
		4:  "30d34ff976ada3fec17173556f1b3a2798e03db6bce9fc93df6bd370f9044e7c",
		5:  "678da8a46273d17e5f85a12c6c3ca8d254af25ccd3c48245b11d8f80e1dba2c3",
		6:  "95b0ca429b6d237b1c905e8b5756363a242ead7fc0cf53783199d3e9ac317dc8",
		7:  "ae1e63c75bf6bde508d8711689db2d5a36f6619885fb3b0a28c1f26b9710143d",
		8:  "6c0697b86c9a57e5dc5b1a38bf18dc1893d850a62229c95134d931b6f4a9d59a",
		9:  "b600b7bd4d7f0aa72ca12f261d2019bc419500b6fde099bba7c3ccf0448f5860",
		10: "701167503d518151ee73ff2ba32f561194538cc46f630551b50b36a483c77c12",
		11: "b7203bbb965b5e5760704ff495c58a8938e97c52ac068e2b0e7c522a2874b3ed",
		12: "b746b89a15262ecedacf8725148d77be22caa57e4dfb60a209854ecdec98382b",
		13: "c51604f27347d0a1b93acad1e33af53463fceb7b6573960f881f95f29c5628ed",
		14: "531398c2b8fc83a65753fae08666946bb253ac3da6596829e71cc8ccc4ddf62f",
		15: "59f0ff9acfed3619c17f078d21a1362ddd5f1feacb9a2d77626f69040d25de65",
		16: "00cc275529e522d93b41e608692fb6e2271f1aa9300ebe020fcaed4d68d0ff76",
		17: "1dad66acb5e7fecf6c39f4a78526b994748d90045220f446a2e80c1b5fd881e7",
		18: "17622df3406657f199d78a3a1aa1f3a8693baf85aa7707910cc6d6cb44c13ee4",
	}
	migrations := DefaultMigrations()
	if len(migrations) != len(expected) {
		t.Fatalf("migration count = %d, want %d", len(migrations), len(expected))
	}
	if got := migrations[0].CompatibleChecksums; len(got) != 1 || got[0] != "57b378b273f243bae44e92cad161f297248e5226e96fbd7905815aa8604146bc" {
		t.Fatalf("migration 1 compatible checksums = %v", got)
	}
	for _, item := range migrations {
		want, ok := expected[item.Version]
		if !ok {
			t.Fatalf("migration version %d is not registered", item.Version)
		}
		if got := item.Checksum(); got != want {
			t.Fatalf("migration %d (%s) checksum = %s, want %s; create a new migration instead of changing an applied migration", item.Version, item.Name, got, want)
		}
	}
}

func TestDefaultMigrationsResumePublishedVersionOne(t *testing.T) {
	migrations := DefaultMigrations()
	published := migrations[0]
	checksums := append([]string{published.Checksum()}, published.CompatibleChecksums...)
	for _, checksum := range checksums {
		checksum := checksum
		t.Run(checksum[:8], func(t *testing.T) {
			database := openSQLite(t)
			for _, statement := range published.StatementsFor(DialectSQLite) {
				if _, err := database.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			ddl, err := schemaTableDDL(DialectSQLite)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(
				`INSERT INTO schema_migration(version, name, checksum, applied_at, execution_ms) VALUES(?, ?, ?, ?, ?)`,
				published.Version,
				published.Name,
				checksum,
				time.Now().UTC(),
				0,
			); err != nil {
				t.Fatal(err)
			}

			report, err := (Runner{DB: database, Dialect: DialectSQLite, Migrations: migrations}).Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if report.CurrentVersion != int64(len(migrations)) || len(report.Applied) != len(migrations)-1 {
				t.Fatalf("report = %+v", report)
			}
		})
	}
}
