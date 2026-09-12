package migration

import (
	"context"
	"fmt"
)

func applyProtocolCredentialCiphertextSchema(ctx context.Context, exec executor, dialect Dialect) error {
	definitions := map[Dialect]string{
		DialectSQLite:   "TEXT NULL",
		DialectMySQL:    "VARCHAR(512) NULL",
		DialectPostgres: "VARCHAR(512) NULL",
	}
	definition, ok := definitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return ensureColumn(ctx, exec, dialect, "protocol_credential", "code_ciphertext", definition)
}
