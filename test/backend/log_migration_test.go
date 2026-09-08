package migration

import "testing"

func TestLogMigrationChecksum(t *testing.T) {
	items := DefaultMigrations()
	if got := items[len(items)-1].Checksum(); got != "1dad66acb5e7fecf6c39f4a78526b994748d90045220f446a2e80c1b5fd881e7" {
		t.Fatalf("unexpected log migration checksum: %s", got)
	}
}
