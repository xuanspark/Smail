package migration

import (
	"context"
	"fmt"
)

func applyEmailThreadSchema(ctx context.Context, exec executor, dialect Dialect) error {
	var idDefinition string
	switch dialect {
	case DialectSQLite:
		idDefinition = "INTEGER NOT NULL DEFAULT 0"
	case DialectMySQL, DialectPostgres:
		idDefinition = "BIGINT NOT NULL DEFAULT 0"
	default:
		return fmt.Errorf("unsupported email thread dialect %q", dialect)
	}
	if err := ensureColumn(ctx, exec, dialect, "email", "reply_email_id", idDefinition); err != nil {
		return err
	}
	for _, column := range []string{"in_reply_to", "thread_references"} {
		if err := ensureColumn(ctx, exec, dialect, "email", column, "TEXT NULL"); err != nil {
			return err
		}
	}
	return nil
}
