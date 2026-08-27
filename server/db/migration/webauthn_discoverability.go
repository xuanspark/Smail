package migration

import (
	"context"
	"fmt"
)

func applyWebAuthnDiscoverabilitySchema(ctx context.Context, exec executor, dialect Dialect) error {
	definitions := map[Dialect]string{
		DialectSQLite:   `INTEGER NOT NULL DEFAULT 0`,
		DialectMySQL:    `TINYINT(1) NOT NULL DEFAULT 0`,
		DialectPostgres: `BOOLEAN NOT NULL DEFAULT FALSE`,
	}
	definition, ok := definitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return ensureColumn(ctx, exec, dialect, "webauthn_credential", "discoverable", definition)
}
