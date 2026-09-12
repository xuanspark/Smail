package migration

import (
	"context"
	"fmt"
)

func applyTrustedSenderBlacklistSchema(ctx context.Context, exec executor, dialect Dialect) error {
	filterTypeDefinitions := map[Dialect]string{
		DialectSQLite:   "TEXT NOT NULL DEFAULT 'whitelist'",
		DialectMySQL:    "VARCHAR(20) NOT NULL DEFAULT 'whitelist'",
		DialectPostgres: "VARCHAR(20) NOT NULL DEFAULT 'whitelist'",
	}
	updatedAtDefinitions := map[Dialect]string{
		DialectSQLite:   "DATETIME NULL",
		DialectMySQL:    "DATETIME(6) NULL",
		DialectPostgres: "TIMESTAMPTZ NULL",
	}

	ftDef, ok := filterTypeDefinitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q for filter_type", dialect)
	}
	if err := ensureColumn(ctx, exec, dialect, "user_trusted_sender", "filter_type", ftDef); err != nil {
		return err
	}

	upDef, ok := updatedAtDefinitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q for updated_at", dialect)
	}
	return ensureColumn(ctx, exec, dialect, "user_trusted_sender", "updated_at", upDef)
}
