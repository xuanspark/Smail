package migration

import (
	"context"
	"fmt"
)

func applySecuritySetupSchema(ctx context.Context, exec executor, dialect Dialect) error {
	definitions := map[Dialect]string{
		DialectSQLite:   "INTEGER NOT NULL DEFAULT 0",
		DialectMySQL:    "TINYINT NOT NULL DEFAULT 0",
		DialectPostgres: "SMALLINT NOT NULL DEFAULT 0",
	}
	definition, ok := definitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return ensureColumn(ctx, exec, dialect, "user", "security_setup_acknowledged", definition)
}
