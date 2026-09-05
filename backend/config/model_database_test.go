package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDatabaseTypeDefaultsAndValidation(t *testing.T) {
	secret := StoredSecret{Ciphertext: "enc:v1:stored", Version: 1, UpdatedAt: time.Now().UTC()}
	tests := []struct {
		name      string
		typeValue DatabaseType
		port      uint16
		wantPort  uint16
	}{
		{name: "mysql default", typeValue: DatabaseTypeMySQL, wantPort: DefaultMySQLPort},
		{name: "postgresql default", typeValue: DatabaseTypePostgreSQL, wantPort: DefaultPostgreSQLPort},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeDatabaseConfig(DatabaseConfig{Type: test.typeValue})
			if got.Port != test.wantPort {
				t.Fatalf("default port = %d, want %d", got.Port, test.wantPort)
			}
			valid := got
			valid.Host = "127.0.0.1"
			valid.Username = "smail"
			valid.Password = secret
			valid.Name = "smail"
			if err := ValidateDatabaseConfig(valid); err != nil {
				t.Fatalf("valid database rejected: %v", err)
			}
		})
	}
	if _, ok := DefaultDatabasePort(DatabaseTypeSQLite); ok {
		t.Fatal("SQLite unexpectedly received a network default port")
	}
	if DatabaseType("postgres").Valid() {
		t.Fatal("legacy PostgreSQL enum was accepted")
	}
}

func TestSQLiteLocalRefIsBackendReferenceOnly(t *testing.T) {
	valid := DatabaseConfig{Type: DatabaseTypeSQLite, LocalRef: "smail.db"}
	if err := ValidateDatabaseConfig(valid); err != nil {
		t.Fatalf("valid SQLite database rejected: %v", err)
	}
	for _, localRef := range []string{
		"../smail.db",
		"config/smail.db",
		"C:\\data\\smail.db",
		"/var/lib/smail/smail.db",
		"\\\\server\\share\\smail.db",
		"",
	} {
		t.Run(localRef, func(t *testing.T) {
			candidate := valid
			candidate.LocalRef = localRef
			if err := ValidateDatabaseConfig(candidate); err == nil {
				t.Fatalf("arbitrary localRef %q was accepted", localRef)
			}
		})
	}
	for name, mutate := range map[string]func(*DatabaseConfig){
		"host":     func(candidate *DatabaseConfig) { candidate.Host = "127.0.0.1" },
		"port":     func(candidate *DatabaseConfig) { candidate.Port = 3306 },
		"username": func(candidate *DatabaseConfig) { candidate.Username = "smail" },
		"name":     func(candidate *DatabaseConfig) { candidate.Name = "smail" },
		"password": func(candidate *DatabaseConfig) {
			candidate.Password = StoredSecret{Ciphertext: "enc:v1:secret", Version: 1, UpdatedAt: time.Now().UTC()}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := ValidateDatabaseConfig(candidate); err == nil {
				t.Fatalf("SQLite %s field was accepted", name)
			}
		})
	}
}

func TestDatabaseModelSerializationUsesLocalRef(t *testing.T) {
	root := testRootConfig()
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	if !strings.Contains(encoded, `"localRef":"smail.db"`) {
		t.Fatalf("serialized database omitted localRef: %s", encoded)
	}
	if strings.Contains(encoded, "sqliteLocalRef") {
		t.Fatalf("serialized database contains legacy SQLite field: %s", encoded)
	}
	var decoded RootConfig
	if err := ParseStrict(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Database.LocalRef != "smail.db" {
		t.Fatalf("decoded localRef = %q", decoded.Database.LocalRef)
	}
}
