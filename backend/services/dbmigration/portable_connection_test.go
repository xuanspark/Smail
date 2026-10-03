package dbmigration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	mysql "github.com/go-sql-driver/mysql"
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

func TestDatabaseCreationErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name string
		kind config.DatabaseType
		err  error
		want error
	}{
		{"postgres permission", config.DatabaseTypePostgreSQL, &pq.Error{Code: "42501"}, ErrSQLPermissionDenied},
		{"postgres wrapped permission", config.DatabaseTypePostgreSQL, fmt.Errorf("private detail: %w", &pq.Error{Code: "42501"}), ErrSQLPermissionDenied},
		{"postgres duplicate", config.DatabaseTypePostgreSQL, &pq.Error{Code: "42P04"}, ErrSQLTargetExists},
		{"mysql database permission", config.DatabaseTypeMySQL, &mysql.MySQLError{Number: 1044}, ErrSQLPermissionDenied},
		{"mysql command permission", config.DatabaseTypeMySQL, &mysql.MySQLError{Number: 1142}, ErrSQLPermissionDenied},
		{"mysql privilege", config.DatabaseTypeMySQL, &mysql.MySQLError{Number: 1227}, ErrSQLPermissionDenied},
		{"mysql duplicate", config.DatabaseTypeMySQL, &mysql.MySQLError{Number: 1007}, ErrSQLTargetExists},
		{"authentication", config.DatabaseTypePostgreSQL, &pq.Error{Code: "28P01"}, ErrSQLConnectionUnavailable},
		{"network", config.DatabaseTypePostgreSQL, errors.New("private connection detail"), ErrSQLConnectionUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyDatabaseCreationError(test.err, test.kind); got != test.want {
				t.Fatalf("classification = %v, want %v", got, test.want)
			}
		})
	}
}

type permissionCatalog struct {
	exists    int64
	allowed   bool
	queryErr  error
	createErr error
	queries   int
	creates   int
}

func (c *permissionCatalog) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c *permissionCatalog) Driver() driver.Driver                        { return c }
func (c *permissionCatalog) Open(string) (driver.Conn, error)             { return c, nil }
func (c *permissionCatalog) Close() error                                 { return nil }
func (c *permissionCatalog) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *permissionCatalog) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *permissionCatalog) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case "SELECT COUNT(*) FROM pg_database WHERE datname = $1":
		if len(args) != 1 || args[0].Value != "smail_next" {
			return nil, errors.New("unexpected target name")
		}
		return &permissionCatalogRow{value: c.exists}, nil
	case "SELECT rolcreatedb OR rolsuper FROM pg_roles WHERE rolname = CURRENT_USER":
		c.queries++
		if c.queryErr != nil {
			return nil, c.queryErr
		}
		return &permissionCatalogRow{value: c.allowed}, nil
	default:
		return nil, errors.New("unexpected catalog query")
	}
}
func (c *permissionCatalog) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(query, "CREATE DATABASE ") {
		return nil, errors.New("unexpected write")
	}
	c.creates++
	return nil, c.createErr
}

type permissionCatalogRow struct {
	value driver.Value
	done  bool
}

func (r *permissionCatalogRow) Columns() []string { return []string{"value"} }
func (r *permissionCatalogRow) Close() error      { return nil }
func (r *permissionCatalogRow) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = r.value
	return nil
}

func TestPortablePostgresPermissionPreviewIsReadOnly(t *testing.T) {
	for _, test := range []struct {
		name    string
		catalog permissionCatalog
		want    error
	}{
		{"denied", permissionCatalog{}, ErrSQLPermissionDenied},
		{"allowed", permissionCatalog{allowed: true}, nil},
		{"existing", permissionCatalog{exists: 1}, ErrSQLTargetExists},
		{"catalog unavailable", permissionCatalog{queryErr: errors.New("unavailable")}, ErrSQLConnectionUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, source := portableFixture(t)
			target := testDatabase(t)
			target.Type = config.DatabaseTypePostgreSQL
			target.Name = "smail_next"
			plan, err := buildTargetPlan(source, target)
			if err != nil {
				t.Fatal(err)
			}
			executor := &PortableExecutor{root: root, open: func(context.Context, config.DatabaseConfig, string) (*sql.DB, error) {
				return sql.OpenDB(&test.catalog), nil
			}}
			if err := executor.Preview(context.Background(), plan); !errors.Is(err, test.want) {
				t.Fatalf("preview = %v, want %v", err, test.want)
			}
			if test.catalog.creates != 0 || executor.paused.Load() || executor.pending {
				t.Fatal("preview wrote target or paused source")
			}
		})
	}
}

func TestPortablePostgresCreationHandlesPermissionRevocation(t *testing.T) {
	catalog := &permissionCatalog{allowed: true, createErr: &pq.Error{Code: "42501", Message: "private database detail"}}
	executor := &PortableExecutor{open: func(context.Context, config.DatabaseConfig, string) (*sql.DB, error) {
		return sql.OpenDB(catalog), nil
	}}
	target := testDatabase(t)
	target.Type = config.DatabaseTypePostgreSQL
	target.Name = "smail_next"
	connection, err := executor.createTarget(context.Background(), target)
	if connection != nil || err != ErrSQLPermissionDenied || catalog.creates != 1 || catalog.queries != 1 {
		t.Fatalf("creation did not preserve permission failure: %v", err)
	}
}
