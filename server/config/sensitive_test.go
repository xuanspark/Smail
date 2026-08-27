package config

import "testing"

func TestSensitiveFieldCatalog(t *testing.T) {
	fields := SensitiveFields()
	if len(fields) != 14 {
		t.Fatalf("unexpected sensitive field count: %d", len(fields))
	}
	seen := map[string]struct{}{}
	for _, field := range fields {
		if _, ok := seen[field.Path]; ok {
			t.Fatalf("duplicate sensitive field: %s", field.Path)
		}
		seen[field.Path] = struct{}{}
		switch field.Policy {
		case PersistenceEncryptedOnly, PersistenceReferenceOnly, PersistenceMemoryOnly:
		default:
			t.Fatalf("invalid policy for %s", field.Path)
		}
		if CanGenericSerialize(field.Path) {
			t.Fatalf("sensitive field can be generically serialized: %s", field.Path)
		}
	}
}

func TestSensitiveWildcard(t *testing.T) {
	field, ok := ClassifySensitive("plugins.telegram.token")
	if !ok || field.Policy != PersistenceEncryptedOnly {
		t.Fatal("plugin token was not classified")
	}
	if _, ok := ClassifySensitive("plugins.telegram.display_name"); ok {
		t.Fatal("non-sensitive plugin field was classified")
	}
}

func TestSensitivePersistenceRules(t *testing.T) {
	tests := []struct {
		path      string
		encrypted bool
		want      bool
	}{
		{path: "database.password", encrypted: false, want: false},
		{path: "database.password", encrypted: true, want: true},
		{path: "ssl.private_key", encrypted: true, want: false},
		{path: "verification.code", encrypted: true, want: false},
		{path: "verification.code_hmac", encrypted: true, want: false},
		{path: "verification.process_key", encrypted: true, want: false},
		{path: "public.display_name", encrypted: false, want: true},
	}
	for _, test := range tests {
		if got := CanPersist(test.path, test.encrypted); got != test.want {
			t.Fatalf("CanPersist(%q, %t) = %t, want %t", test.path, test.encrypted, got, test.want)
		}
	}
}
