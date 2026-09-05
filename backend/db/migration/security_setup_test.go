package migration

import (
	"context"
	"testing"
)

func TestSecuritySetupMigrationPreservesAcknowledgement(t *testing.T) {
	db := openSQLite(t)
	runner := Runner{DB: db, Dialect: DialectSQLite, Migrations: DefaultMigrations()[:14]}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO "user" (account, name, password) VALUES ('root', 'Root', 'unchanged')`); err != nil {
		t.Fatal(err)
	}
	runner.Migrations = DefaultMigrations()
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var acknowledged int
	if err := db.QueryRow(`SELECT security_setup_acknowledged FROM "user" WHERE account='root'`).Scan(&acknowledged); err != nil {
		t.Fatal(err)
	}
	if acknowledged != 0 {
		t.Fatal("existing account must receive the new guide once")
	}
	if _, err := db.Exec(`UPDATE "user" SET security_setup_acknowledged=1 WHERE account='root'`); err != nil {
		t.Fatal(err)
	}
	if err := applySecuritySetupSchema(context.Background(), db, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT security_setup_acknowledged FROM "user" WHERE account='root'`).Scan(&acknowledged); err != nil {
		t.Fatal(err)
	}
	if acknowledged != 1 {
		t.Fatal("repeated migration reset acknowledgement")
	}
}
