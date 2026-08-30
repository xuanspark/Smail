package migration

import (
	"context"
	"fmt"
)

func applyUserEmailAddressSchema(ctx context.Context, exec executor, dialect Dialect) error {
	columns := []struct {
		name       string
		definition map[Dialect]string
	}{
		{name: "address", definition: map[Dialect]string{
			DialectSQLite:   `TEXT NULL`,
			DialectMySQL:    `VARCHAR(254) NULL`,
			DialectPostgres: `VARCHAR(254) NULL`,
		}},
		{name: "address_canonical", definition: map[Dialect]string{
			DialectSQLite:   `TEXT NULL`,
			DialectMySQL:    `VARCHAR(254) NULL`,
			DialectPostgres: `VARCHAR(254) NULL`,
		}},
		{name: "is_primary", definition: map[Dialect]string{
			DialectSQLite:   `INTEGER NOT NULL DEFAULT 0`,
			DialectMySQL:    `TINYINT NOT NULL DEFAULT 0`,
			DialectPostgres: `SMALLINT NOT NULL DEFAULT 0`,
		}},
		{name: "verified_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NULL`,
			DialectMySQL:    `DATETIME(6) NULL`,
			DialectPostgres: `TIMESTAMPTZ NULL`,
		}},
		{name: "created_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NULL`,
			DialectMySQL:    `DATETIME(6) NULL`,
			DialectPostgres: `TIMESTAMPTZ NULL`,
		}},
		{name: "updated_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NULL`,
			DialectMySQL:    `DATETIME(6) NULL`,
			DialectPostgres: `TIMESTAMPTZ NULL`,
		}},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, exec, dialect, "user_email", column.name, column.definition[dialect]); err != nil {
			return err
		}
	}

	var conflicts int
	conflictQuery := `SELECT COUNT(*) FROM (SELECT "address_canonical" FROM "user_email" WHERE "address_canonical" IS NOT NULL AND TRIM("address_canonical") <> '' GROUP BY "address_canonical" HAVING COUNT(*) > 1) AS conflicts`
	if dialect == DialectMySQL {
		conflictQuery = "SELECT COUNT(*) FROM (SELECT `address_canonical` FROM `user_email` WHERE `address_canonical` IS NOT NULL AND TRIM(`address_canonical`) <> '' GROUP BY `address_canonical` HAVING COUNT(*) > 1) AS conflicts"
	}
	if dialect == DialectPostgres {
		conflictQuery = `SELECT COUNT(*) FROM (SELECT "address_canonical" FROM "user_email" WHERE "address_canonical" IS NOT NULL AND BTRIM("address_canonical") <> '' GROUP BY "address_canonical" HAVING COUNT(*) > 1) AS conflicts`
	}
	if err := exec.QueryRowContext(ctx, conflictQuery).Scan(&conflicts); err != nil {
		return fmt.Errorf("check user email address conflicts: %w", err)
	}
	if conflicts > 0 {
		return fmt.Errorf("user email address conflict count: %d", conflicts)
	}

	if err := ensureIndex(ctx, exec, dialect, "user_email", "uq_user_email_address_canonical", true, []string{"address_canonical"}); err != nil {
		return err
	}
	return ensureIndex(ctx, exec, dialect, "user_email", "idx_user_email_user_primary", false, []string{"user_id", "is_primary"})
}
