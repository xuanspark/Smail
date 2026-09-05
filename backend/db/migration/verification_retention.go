package migration

import (
	"context"
	"fmt"
)

func applyVerificationRetentionSchema(ctx context.Context, exec executor, dialect Dialect) error {
	columns := []struct {
		table      string
		name       string
		definition map[Dialect]string
	}{
		{table: "verification_rate_limit", name: "expires_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00'`,
			DialectMySQL:    `DATETIME(6) NOT NULL DEFAULT '1970-01-01 00:00:00'`,
			DialectPostgres: `TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00'`,
		}},
		{table: "verification_event", name: "expires_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00'`,
			DialectMySQL:    `DATETIME(6) NOT NULL DEFAULT '1970-01-01 00:00:00'`,
			DialectPostgres: `TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00'`,
		}},
		{table: "verification_event", name: "retention_class", definition: map[Dialect]string{
			DialectSQLite:   `TEXT NOT NULL DEFAULT 'detail'`,
			DialectMySQL:    `VARCHAR(16) NOT NULL DEFAULT 'detail'`,
			DialectPostgres: `VARCHAR(16) NOT NULL DEFAULT 'detail'`,
		}},
		{table: "verification_event", name: "investigation_hold", definition: map[Dialect]string{
			DialectSQLite:   `INTEGER NOT NULL DEFAULT 0`,
			DialectMySQL:    `TINYINT NOT NULL DEFAULT 0`,
			DialectPostgres: `BOOLEAN NOT NULL DEFAULT FALSE`,
		}},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, exec, dialect, column.table, column.name, column.definition[dialect]); err != nil {
			return err
		}
	}

	updates := map[Dialect][]string{
		DialectSQLite: {
			`UPDATE verification_rate_limit SET expires_at = datetime(updated_at, '+30 days') WHERE expires_at = '1970-01-01 00:00:00'`,
			`UPDATE verification_event SET expires_at = datetime(created_at, '+30 days') WHERE expires_at = '1970-01-01 00:00:00'`,
		},
		DialectMySQL: {
			`UPDATE verification_rate_limit SET expires_at = DATE_ADD(updated_at, INTERVAL 30 DAY) WHERE expires_at = '1970-01-01 00:00:00'`,
			`UPDATE verification_event SET expires_at = DATE_ADD(created_at, INTERVAL 30 DAY) WHERE expires_at = '1970-01-01 00:00:00'`,
		},
		DialectPostgres: {
			`UPDATE verification_rate_limit SET expires_at = updated_at + INTERVAL '30 days' WHERE expires_at = '1970-01-01 00:00:00+00'`,
			`UPDATE verification_event SET expires_at = created_at + INTERVAL '30 days' WHERE expires_at = '1970-01-01 00:00:00+00'`,
		},
	}
	for _, statement := range updates[dialect] {
		if _, err := exec.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill verification retention: %w", err)
		}
	}

	return createVerificationRetentionIndexes(ctx, exec, dialect)
}

func createVerificationRetentionIndexes(ctx context.Context, exec executor, dialect Dialect) error {
	statement := `CREATE INDEX IF NOT EXISTS "idx_verification_event_expires_at" ON "verification_event"("expires_at")`
	if dialect == DialectMySQL {
		var count int
		if err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?", "verification_event", "idx_verification_event_expires_at").Scan(&count); err != nil {
			return fmt.Errorf("check verification retention index: %w", err)
		}
		if count == 0 {
			if _, err := exec.ExecContext(ctx, "CREATE INDEX `idx_verification_event_expires_at` ON `verification_event` (`expires_at`)"); err != nil {
				return fmt.Errorf("create verification retention index: %w", err)
			}
		}
		return nil
	}
	if _, err := exec.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create verification retention index: %w", err)
	}
	return nil
}
