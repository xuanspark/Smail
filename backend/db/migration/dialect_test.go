package migration

import "testing"

func TestDialectSQL(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectSQLite, DialectPostgres} {
		if _, err := schemaTableDDL(dialect); err != nil {
			t.Fatalf("DDL for %s: %v", dialect, err)
		}
		if _, err := insertSQL(dialect); err != nil {
			t.Fatalf("insert SQL for %s: %v", dialect, err)
		}
		if parsed, err := ParseDialect(string(dialect)); err != nil || parsed != dialect {
			t.Fatalf("ParseDialect(%s) = %s, %v", dialect, parsed, err)
		}
	}
	if _, err := ParseDialect("oracle"); err == nil {
		t.Fatal("unsupported dialect accepted")
	}
}

func TestMigrationChecksumIsStableAndContentBound(t *testing.T) {
	base := Define(1, "create", "CREATE TABLE sample(id INTEGER)")
	copyValue := Define(1, "create", "CREATE TABLE sample(id INTEGER)")
	changed := Define(1, "create", "CREATE TABLE sample(id BIGINT)")
	if base.Checksum() != copyValue.Checksum() {
		t.Fatal("equivalent migrations have different checksums")
	}
	if base.Checksum() == changed.Checksum() {
		t.Fatal("changed migration retained checksum")
	}
}
