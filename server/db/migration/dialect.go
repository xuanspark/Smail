package migration

import "fmt"

type Dialect string

const (
	DialectMySQL    Dialect = "mysql"
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

func ParseDialect(value string) (Dialect, error) {
	dialect := Dialect(value)
	switch dialect {
	case DialectMySQL, DialectSQLite, DialectPostgres:
		return dialect, nil
	default:
		return "", fmt.Errorf("unsupported migration dialect %q", value)
	}
}

func schemaTableDDL(dialect Dialect) (string, error) {
	switch dialect {
	case DialectMySQL:
		return "CREATE TABLE IF NOT EXISTS schema_migration (version BIGINT NOT NULL PRIMARY KEY, name VARCHAR(255) NOT NULL, checksum CHAR(64) NOT NULL, applied_at TIMESTAMP(6) NOT NULL, execution_ms BIGINT NOT NULL)", nil
	case DialectSQLite:
		return "CREATE TABLE IF NOT EXISTS schema_migration (version INTEGER NOT NULL PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TIMESTAMP NOT NULL, execution_ms INTEGER NOT NULL)", nil
	case DialectPostgres:
		return "CREATE TABLE IF NOT EXISTS schema_migration (version BIGINT PRIMARY KEY, name VARCHAR(255) NOT NULL, checksum CHAR(64) NOT NULL, applied_at TIMESTAMPTZ NOT NULL, execution_ms BIGINT NOT NULL)", nil
	default:
		return "", fmt.Errorf("unsupported migration dialect %q", dialect)
	}
}

func insertSQL(dialect Dialect) (string, error) {
	switch dialect {
	case DialectMySQL, DialectSQLite:
		return "INSERT INTO schema_migration(version, name, checksum, applied_at, execution_ms) VALUES (?, ?, ?, ?, ?)", nil
	case DialectPostgres:
		return "INSERT INTO schema_migration(version, name, checksum, applied_at, execution_ms) VALUES ($1, $2, $3, $4, $5)", nil
	default:
		return "", fmt.Errorf("unsupported migration dialect %q", dialect)
	}
}
