package dbmigration

import (
	"net/url"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"github.com/lib/pq"
)

func TestPortablePostgresUsesSupportedRuntimeTLSMode(t *testing.T) {
	for _, name := range []string{"", "smail_next"} {
		driver, dsn, err := runtimeSQLDSN(config.DatabaseConfig{
			Type: config.DatabaseTypePostgreSQL, Host: "::1", Port: 5432, Username: "review",
		}, "test-only-password", name)
		if err != nil || driver != "postgres" {
			t.Fatal("PostgreSQL connection was not built")
		}
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("sslmode") != "require" || query.Get("timezone") != "UTC" || query.Get("search_path") != "public" {
			t.Fatal("migration connection differs from supported runtime TLS or time settings")
		}
		if _, err := pq.NewConnector(dsn); err != nil {
			t.Fatal("driver rejected generated connection parameters")
		}
		if name == "" && parsed.Path != "/postgres" {
			t.Fatal("maintenance connection did not select postgres database")
		}
	}
}
