package migration

import (
	"context"
	"fmt"
	"strings"
)

func ensureIndex(ctx context.Context, exec executor, dialect Dialect, table, index string, unique bool, columns []string) error {
	exists, err := indexExists(ctx, exec, dialect, table, index)
	if err != nil {
		return fmt.Errorf("check index %s: %w", index, err)
	}
	if exists {
		return nil
	}
	quotedTable, quotedIndex, err := quotedIdentifiers(dialect, table, index)
	if err != nil {
		return err
	}
	quotedColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		_, quotedColumn, err := quotedIdentifiers(dialect, table, column)
		if err != nil {
			return err
		}
		quotedColumns = append(quotedColumns, quotedColumn)
	}
	indexType := "INDEX"
	if unique {
		indexType = "UNIQUE INDEX"
	}
	statement := fmt.Sprintf("CREATE %s %s ON %s (%s)", indexType, quotedIndex, quotedTable, strings.Join(quotedColumns, ", "))
	if _, err := exec.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create index %s: %w", index, err)
	}
	return nil
}

func indexExists(ctx context.Context, exec executor, dialect Dialect, table, index string) (bool, error) {
	var count int
	switch dialect {
	case DialectSQLite:
		if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ?`, table, index).Scan(&count); err != nil {
			return false, err
		}
	case DialectMySQL:
		if err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?", table, index).Scan(&count); err != nil {
			return false, err
		}
	case DialectPostgres:
		if err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1 AND indexname = $2", table, index).Scan(&count); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported index dialect %q", dialect)
	}
	return count > 0, nil
}
