package migration

import "testing"

func TestLogMigrationChecksum(t *testing.T) {
	items := DefaultMigrations()
	if got := items[16].Checksum(); got != "1dad66acb5e7fecf6c39f4a78526b994748d90045220f446a2e80c1b5fd881e7" {
		t.Fatalf("unexpected log migration checksum: %s", got)
	}
	if got := items[17].Checksum(); got != "17622df3406657f199d78a3a1aa1f3a8693baf85aa7707910cc6d6cb44c13ee4" {
		t.Fatalf("unexpected operation log role migration checksum: %s", got)
	}
}
