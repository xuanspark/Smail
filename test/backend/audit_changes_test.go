package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditChangesRedactPluginArraysAndSettings(t *testing.T) {
	before := RuntimeConfig{}
	after := RuntimeConfig{
		Root: RootConfig{Database: DatabaseConfig{Password: StoredSecret{Ciphertext: "hidden-cipher"}}},
		Plugins: PluginsSectionConfig{Plugins: map[string]PluginInstance{
			"sample": {Settings: json.RawMessage(`{"items":[{"password":"hidden-password"}]}`)},
		}},
	}
	changes := AuditChanges(before, after)
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hidden-") {
		t.Fatalf("secret leaked: %s", data)
	}
	fields := map[string]any{}
	flattenAudit(fields, "array", []any{map[string]any{"password": "hidden-password"}})
	if !auditSensitive("array.0.password") || fields["array.0.password"] == nil {
		t.Fatal("array traversal failed")
	}
}
