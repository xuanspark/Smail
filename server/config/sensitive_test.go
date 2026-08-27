package config

import "testing"

func TestSensitiveFieldCatalog(t *testing.T) {
	fields := SensitiveFields()
	if len(fields) < 30 {
		t.Fatalf("sensitive field catalog is incomplete: %d", len(fields))
	}
	seen := map[string]struct{}{}
	for _, field := range fields {
		if _, ok := seen[field.Path]; ok {
			t.Fatalf("duplicate sensitive field: %s", field.Path)
		}
		seen[field.Path] = struct{}{}
		switch field.Policy {
		case PersistenceEncryptedOnly, PersistenceReferenceOnly, PersistenceMemoryOnly, PersistenceHashedOnly:
		default:
			t.Fatalf("invalid policy for %s", field.Path)
		}
		if field.Description == "" {
			t.Fatalf("missing description for %s", field.Path)
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
		{path: "verification.account_fingerprint", encrypted: true, want: false},
	}
	for _, test := range tests {
		if got := CanPersist(test.path, test.encrypted); got != test.want {
			t.Fatalf("CanPersist(%q, %t) = %t, want %t", test.path, test.encrypted, got, test.want)
		}
	}
}

func TestSensitiveRepresentations(t *testing.T) {
	tests := []struct {
		path           string
		representation string
		want           bool
	}{
		{path: "database.password", representation: "encrypted", want: true},
		{path: "database.password", representation: "hash", want: false},
		{path: "verification.code", representation: "hash", want: false},
		{path: "verification.code", representation: "memory", want: true},
		{path: "verification.email_fingerprint", representation: "hash", want: true},
		{path: "ssl.private_key", representation: "reference", want: true},
	}
	for _, test := range tests {
		if got := CanPersistRepresentation(test.path, test.representation); got != test.want {
			t.Fatalf("CanPersistRepresentation(%q, %q) = %t, want %t", test.path, test.representation, got, test.want)
		}
	}
}

func TestRedactPublicValue(t *testing.T) {
	value := map[string]any{
		"database": map[string]any{
			"password": map[string]any{"ciphertext": "enc-secret"},
			"name":     "smail",
		},
		"verification": map[string]any{
			"code":              "12345678",
			"email_fingerprint": "fingerprint",
		},
	}
	redacted, ok := RedactPublicValue(value).(map[string]any)
	if !ok {
		t.Fatalf("unexpected redacted value type: %T", RedactPublicValue(value))
	}
	database := redacted["database"].(map[string]any)
	if database["password"] != RedactedValue || database["name"] != "smail" {
		t.Fatalf("database public view was not redacted: %#v", database)
	}
	verification := redacted["verification"].(map[string]any)
	if verification["code"] != RedactedValue || verification["email_fingerprint"] != RedactedValue {
		t.Fatalf("verification public view was not redacted: %#v", verification)
	}
}
