package migration

import (
	"context"
	"fmt"
	"strings"
)

func coreSchemaStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "user" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "account" TEXT NOT NULL UNIQUE, "name" TEXT NOT NULL, "password" TEXT NOT NULL, "disabled" INTEGER NOT NULL DEFAULT 0, "role" TEXT NOT NULL DEFAULT 'user', "is_admin" INTEGER NOT NULL DEFAULT 0, "session_version" INTEGER NOT NULL DEFAULT 1, "security_version" INTEGER NOT NULL DEFAULT 1, "must_change_password" INTEGER NOT NULL DEFAULT 0, "password_changed_at" DATETIME NULL)`,
			`CREATE TABLE IF NOT EXISTS "email" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "type" INTEGER NOT NULL DEFAULT 0, "subject" TEXT NOT NULL DEFAULT '', "reply_to" TEXT NULL, "from_name" TEXT NOT NULL DEFAULT '', "from_address" TEXT NOT NULL DEFAULT '', "to" TEXT NULL, "bcc" TEXT NULL, "cc" TEXT NULL, "text" TEXT NULL, "html" TEXT NULL, "sender" TEXT NULL, "attachments" TEXT NULL, "spf_check" INTEGER NOT NULL DEFAULT 0, "dkim_check" INTEGER NOT NULL DEFAULT 0, "status" INTEGER NOT NULL DEFAULT 0, "cron_send_time" DATETIME NULL, "update_time" DATETIME NULL, "send_user_id" INTEGER NOT NULL DEFAULT 0, "size" INTEGER NOT NULL DEFAULT 1000, "error" TEXT NULL, "send_date" DATETIME NULL, "create_time" DATETIME NULL, "msg_id" TEXT NOT NULL DEFAULT '')`,
			`CREATE TABLE IF NOT EXISTS "group" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "name" TEXT NOT NULL DEFAULT '', "parent_id" INTEGER NOT NULL DEFAULT 0, "user_id" INTEGER NOT NULL DEFAULT 0, "full_path" TEXT NULL)`,
			`CREATE TABLE IF NOT EXISTS "rule" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL DEFAULT 0, "name" TEXT NOT NULL DEFAULT '', "value" TEXT NULL, "action" INTEGER NOT NULL DEFAULT 0, "params" TEXT NOT NULL DEFAULT '', "sort" INTEGER NOT NULL DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS "sessions" ("token" TEXT PRIMARY KEY, "data" BLOB NOT NULL, "expiry" REAL NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_sessions_expiry" ON "sessions"("expiry")`,
			`CREATE TABLE IF NOT EXISTS "user_email" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email_id" INTEGER NOT NULL, "is_read" INTEGER NOT NULL DEFAULT 0, "group_id" INTEGER NOT NULL DEFAULT 0, "status" INTEGER NOT NULL DEFAULT 0, "create" DATETIME NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_eid" ON "user_email"("user_id", "email_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_create_time" ON "user_email"("create")`,
			`CREATE TABLE IF NOT EXISTS "version" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "info" TEXT NOT NULL)`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `user` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `account` VARCHAR(20) NOT NULL, `name` VARCHAR(10) NOT NULL, `password` VARCHAR(255) NOT NULL, `disabled` INT UNSIGNED NOT NULL DEFAULT 0, `role` VARCHAR(20) NOT NULL DEFAULT 'user', `is_admin` INT UNSIGNED NOT NULL DEFAULT 0, `session_version` INT UNSIGNED NOT NULL DEFAULT 1, `security_version` INT UNSIGNED NOT NULL DEFAULT 1, `must_change_password` INT UNSIGNED NOT NULL DEFAULT 0, `password_changed_at` DATETIME(6) NULL, UNIQUE KEY `uq_user_account` (`account`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `email` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `type` TINYINT NOT NULL DEFAULT 0, `subject` VARCHAR(1000) NOT NULL DEFAULT '', `reply_to` TEXT NULL, `from_name` VARCHAR(50) NOT NULL DEFAULT '', `from_address` VARCHAR(100) NOT NULL DEFAULT '', `to` TEXT NULL, `bcc` TEXT NULL, `cc` TEXT NULL, `text` TEXT NULL, `html` MEDIUMTEXT NULL, `sender` TEXT NULL, `attachments` LONGTEXT NULL, `spf_check` TINYINT NOT NULL DEFAULT 0, `dkim_check` TINYINT NOT NULL DEFAULT 0, `status` TINYINT NOT NULL DEFAULT 0, `cron_send_time` DATETIME(6) NULL, `update_time` DATETIME(6) NULL, `send_user_id` INT UNSIGNED NOT NULL DEFAULT 0, `size` INT UNSIGNED NOT NULL DEFAULT 1000, `error` TEXT NULL, `send_date` DATETIME(6) NULL, `create_time` DATETIME(6) NULL, `msg_id` VARCHAR(255) NOT NULL DEFAULT '') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `group` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `name` VARCHAR(10) NOT NULL DEFAULT '', `parent_id` INT UNSIGNED NOT NULL DEFAULT 0, `user_id` INT UNSIGNED NOT NULL DEFAULT 0, `full_path` VARCHAR(600) NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `rule` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` INT NOT NULL DEFAULT 0, `name` VARCHAR(255) NOT NULL DEFAULT '', `value` TEXT NULL, `action` INT NOT NULL DEFAULT 0, `params` VARCHAR(255) NOT NULL DEFAULT '', `sort` INT NOT NULL DEFAULT 0) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `sessions` (`token` CHAR(43) NOT NULL PRIMARY KEY, `data` BLOB NOT NULL, `expiry` TIMESTAMP(6) NOT NULL, KEY `idx_sessions_expiry` (`expiry`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `user_email` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` INT NOT NULL, `email_id` INT NOT NULL, `is_read` TINYINT NOT NULL DEFAULT 0, `group_id` INT NOT NULL DEFAULT 0, `status` TINYINT NOT NULL DEFAULT 0, `create` DATETIME(6) NULL, KEY `idx_eid` (`user_id`, `email_id`), KEY `idx_create_time` (`create`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `version` (`id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `info` VARCHAR(255) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "user" ("id" BIGSERIAL PRIMARY KEY, "account" VARCHAR(20) NOT NULL UNIQUE, "name" VARCHAR(10) NOT NULL, "password" VARCHAR(255) NOT NULL, "disabled" INTEGER NOT NULL DEFAULT 0, "role" VARCHAR(20) NOT NULL DEFAULT 'user', "is_admin" INTEGER NOT NULL DEFAULT 0, "session_version" INTEGER NOT NULL DEFAULT 1, "security_version" INTEGER NOT NULL DEFAULT 1, "must_change_password" INTEGER NOT NULL DEFAULT 0, "password_changed_at" TIMESTAMP NULL)`,
			`CREATE TABLE IF NOT EXISTS "email" ("id" BIGSERIAL PRIMARY KEY, "type" SMALLINT NOT NULL DEFAULT 0, "subject" VARCHAR(1000) NOT NULL DEFAULT '', "reply_to" TEXT NULL, "from_name" VARCHAR(50) NOT NULL DEFAULT '', "from_address" VARCHAR(100) NOT NULL DEFAULT '', "to" TEXT NULL, "bcc" TEXT NULL, "cc" TEXT NULL, "text" TEXT NULL, "html" TEXT NULL, "sender" TEXT NULL, "attachments" TEXT NULL, "spf_check" SMALLINT NOT NULL DEFAULT 0, "dkim_check" SMALLINT NOT NULL DEFAULT 0, "status" SMALLINT NOT NULL DEFAULT 0, "cron_send_time" TIMESTAMPTZ NULL, "update_time" TIMESTAMPTZ NULL, "send_user_id" BIGINT NOT NULL DEFAULT 0, "size" BIGINT NOT NULL DEFAULT 1000, "error" TEXT NULL, "send_date" TIMESTAMPTZ NULL, "create_time" TIMESTAMPTZ NULL, "msg_id" VARCHAR(255) NOT NULL DEFAULT '')`,
			`CREATE TABLE IF NOT EXISTS "group" ("id" BIGSERIAL PRIMARY KEY, "name" VARCHAR(10) NOT NULL DEFAULT '', "parent_id" BIGINT NOT NULL DEFAULT 0, "user_id" BIGINT NOT NULL DEFAULT 0, "full_path" VARCHAR(600) NULL)`,
			`CREATE TABLE IF NOT EXISTS "rule" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL DEFAULT 0, "name" VARCHAR(255) NOT NULL DEFAULT '', "value" TEXT NULL, "action" INTEGER NOT NULL DEFAULT 0, "params" VARCHAR(255) NOT NULL DEFAULT '', "sort" INTEGER NOT NULL DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS "sessions" ("token" VARCHAR(43) PRIMARY KEY, "data" BYTEA NOT NULL, "expiry" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_sessions_expiry" ON "sessions"("expiry")`,
			`CREATE TABLE IF NOT EXISTS "user_email" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "email_id" BIGINT NOT NULL, "is_read" SMALLINT NOT NULL DEFAULT 0, "group_id" BIGINT NOT NULL DEFAULT 0, "status" SMALLINT NOT NULL DEFAULT 0, "create" TIMESTAMPTZ NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_eid" ON "user_email"("user_id", "email_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_create_time" ON "user_email"("create")`,
			`CREATE TABLE IF NOT EXISTS "version" ("id" BIGSERIAL PRIMARY KEY, "info" VARCHAR(255) NOT NULL)`,
		},
	}
}

func applyUserLifecycleSchema(ctx context.Context, exec executor, dialect Dialect) error {
	columns := []struct {
		name       string
		definition map[Dialect]string
	}{
		{name: "security_version", definition: map[Dialect]string{
			DialectSQLite:   `INTEGER NOT NULL DEFAULT 1`,
			DialectMySQL:    `INT UNSIGNED NOT NULL DEFAULT 1`,
			DialectPostgres: `INTEGER NOT NULL DEFAULT 1`,
		}},
		{name: "must_change_password", definition: map[Dialect]string{
			DialectSQLite:   `INTEGER NOT NULL DEFAULT 0`,
			DialectMySQL:    `INT UNSIGNED NOT NULL DEFAULT 0`,
			DialectPostgres: `INTEGER NOT NULL DEFAULT 0`,
		}},
		{name: "password_changed_at", definition: map[Dialect]string{
			DialectSQLite:   `DATETIME NULL`,
			DialectMySQL:    `DATETIME(6) NULL`,
			DialectPostgres: `TIMESTAMP NULL`,
		}},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, exec, dialect, "user", column.name, column.definition[dialect]); err != nil {
			return err
		}
	}
	return nil
}

func verificationOperationsStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "verification_rate_limit" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "dimension_key" TEXT NOT NULL UNIQUE, "dimension_type" TEXT NOT NULL, "hour_started_at" DATETIME NULL, "hour_count" INTEGER NOT NULL DEFAULT 0, "day_started_at" DATETIME NULL, "day_count" INTEGER NOT NULL DEFAULT 0, "last_issued_at" DATETIME NULL, "updated_at" DATETIME NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_rate_limit_updated_at" ON "verification_rate_limit"("updated_at")`,
			`CREATE TABLE IF NOT EXISTS "verification_cleanup_state" ("state_key" TEXT PRIMARY KEY, "next_run_at" DATETIME NOT NULL, "lease_until" DATETIME NOT NULL, "lease_token" TEXT NOT NULL, "failure_count" INTEGER NOT NULL DEFAULT 0, "updated_at" DATETIME NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_cleanup_state_next_run_at" ON "verification_cleanup_state"("next_run_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `verification_rate_limit` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `dimension_key` CHAR(64) NOT NULL, `dimension_type` VARCHAR(32) NOT NULL, `hour_started_at` DATETIME(6) NULL, `hour_count` INT NOT NULL DEFAULT 0, `day_started_at` DATETIME(6) NULL, `day_count` INT NOT NULL DEFAULT 0, `last_issued_at` DATETIME(6) NULL, `updated_at` DATETIME(6) NOT NULL, UNIQUE KEY `uq_verification_rate_limit_dimension_key` (`dimension_key`), KEY `idx_verification_rate_limit_updated_at` (`updated_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `verification_cleanup_state` (`state_key` VARCHAR(64) NOT NULL PRIMARY KEY, `next_run_at` DATETIME(6) NOT NULL, `lease_until` DATETIME(6) NOT NULL, `lease_token` CHAR(64) NOT NULL, `failure_count` INT NOT NULL DEFAULT 0, `updated_at` DATETIME(6) NOT NULL, KEY `idx_verification_cleanup_state_next_run_at` (`next_run_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "verification_rate_limit" ("id" BIGSERIAL PRIMARY KEY, "dimension_key" CHAR(64) NOT NULL UNIQUE, "dimension_type" VARCHAR(32) NOT NULL, "hour_started_at" TIMESTAMPTZ NULL, "hour_count" INTEGER NOT NULL DEFAULT 0, "day_started_at" TIMESTAMPTZ NULL, "day_count" INTEGER NOT NULL DEFAULT 0, "last_issued_at" TIMESTAMPTZ NULL, "updated_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_rate_limit_updated_at" ON "verification_rate_limit"("updated_at")`,
			`CREATE TABLE IF NOT EXISTS "verification_cleanup_state" ("state_key" VARCHAR(64) PRIMARY KEY, "next_run_at" TIMESTAMPTZ NOT NULL, "lease_until" TIMESTAMPTZ NOT NULL, "lease_token" CHAR(64) NOT NULL, "failure_count" INTEGER NOT NULL DEFAULT 0, "updated_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_cleanup_state_next_run_at" ON "verification_cleanup_state"("next_run_at")`,
		},
	}
}

func securityEmailStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "external_email" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email_ciphertext" TEXT NOT NULL, "email_fingerprint" TEXT NOT NULL UNIQUE, "email_type" TEXT NOT NULL, "status" TEXT NOT NULL, "verified_at" DATETIME NULL, "credential_version" INTEGER NOT NULL DEFAULT 1, "created_at" DATETIME NOT NULL, "updated_at" DATETIME NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_external_email_user_id" ON "external_email"("user_id")`,
			`CREATE TABLE IF NOT EXISTS "verification_event" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL DEFAULT 0, "purpose" TEXT NOT NULL, "action" TEXT NOT NULL, "outcome" TEXT NOT NULL, "account_fingerprint" TEXT NULL, "email_fingerprint" TEXT NULL, "ip_fingerprint" TEXT NULL, "created_at" DATETIME NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_event_created_at" ON "verification_event"("created_at")`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_event_user_id" ON "verification_event"("user_id")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `external_email` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `email_ciphertext` VARCHAR(1024) NOT NULL, `email_fingerprint` VARCHAR(80) NOT NULL, `email_type` VARCHAR(16) NOT NULL, `status` VARCHAR(16) NOT NULL, `verified_at` DATETIME(6) NULL, `credential_version` INT NOT NULL DEFAULT 1, `created_at` DATETIME(6) NOT NULL, `updated_at` DATETIME(6) NOT NULL, UNIQUE KEY `uq_external_email_fingerprint` (`email_fingerprint`), KEY `idx_external_email_user_id` (`user_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `verification_event` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL DEFAULT 0, `purpose` VARCHAR(32) NOT NULL, `action` VARCHAR(32) NOT NULL, `outcome` VARCHAR(16) NOT NULL, `account_fingerprint` VARCHAR(80) NULL, `email_fingerprint` VARCHAR(80) NULL, `ip_fingerprint` VARCHAR(80) NULL, `created_at` DATETIME(6) NOT NULL, KEY `idx_verification_event_created_at` (`created_at`), KEY `idx_verification_event_user_id` (`user_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "external_email" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "email_ciphertext" VARCHAR(1024) NOT NULL, "email_fingerprint" VARCHAR(80) NOT NULL UNIQUE, "email_type" VARCHAR(16) NOT NULL, "status" VARCHAR(16) NOT NULL, "verified_at" TIMESTAMPTZ NULL, "credential_version" INTEGER NOT NULL DEFAULT 1, "created_at" TIMESTAMPTZ NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_external_email_user_id" ON "external_email"("user_id")`,
			`CREATE TABLE IF NOT EXISTS "verification_event" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL DEFAULT 0, "purpose" VARCHAR(32) NOT NULL, "action" VARCHAR(32) NOT NULL, "outcome" VARCHAR(16) NOT NULL, "account_fingerprint" VARCHAR(80) NULL, "email_fingerprint" VARCHAR(80) NULL, "ip_fingerprint" VARCHAR(80) NULL, "created_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_event_created_at" ON "verification_event"("created_at")`,
			`CREATE INDEX IF NOT EXISTS "idx_verification_event_user_id" ON "verification_event"("user_id")`,
		},
	}
}

func applyUserSecuritySchema(ctx context.Context, exec executor, dialect Dialect) error {
	columns := []struct {
		name       string
		definition map[Dialect]string
	}{
		{name: "role", definition: map[Dialect]string{
			DialectSQLite:   `TEXT NOT NULL DEFAULT 'user'`,
			DialectMySQL:    `VARCHAR(20) NOT NULL DEFAULT 'user'`,
			DialectPostgres: `VARCHAR(20) NOT NULL DEFAULT 'user'`,
		}},
		{name: "session_version", definition: map[Dialect]string{
			DialectSQLite:   `INTEGER NOT NULL DEFAULT 1`,
			DialectMySQL:    `INT UNSIGNED NOT NULL DEFAULT 1`,
			DialectPostgres: `INTEGER NOT NULL DEFAULT 1`,
		}},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, exec, dialect, "user", column.name, column.definition[dialect]); err != nil {
			return err
		}
	}

	switch dialect {
	case DialectMySQL:
		if _, err := exec.ExecContext(ctx, "ALTER TABLE `user` MODIFY COLUMN `password` VARCHAR(255) NOT NULL"); err != nil {
			return fmt.Errorf("expand user password column: %w", err)
		}
	case DialectPostgres:
		if _, err := exec.ExecContext(ctx, `ALTER TABLE "user" ALTER COLUMN "password" TYPE VARCHAR(255)`); err != nil {
			return fmt.Errorf("expand user password column: %w", err)
		}
	}

	query := `UPDATE "user" SET "role" = CASE WHEN "is_admin" = 1 THEN 'super_admin' ELSE 'user' END WHERE "role" IS NULL OR "role" = '' OR ("is_admin" = 1 AND "role" = 'user')`
	if dialect == DialectMySQL {
		query = "UPDATE `user` SET `role` = CASE WHEN `is_admin` = 1 THEN 'super_admin' ELSE 'user' END WHERE `role` IS NULL OR `role` = '' OR (`is_admin` = 1 AND `role` = 'user')"
	}
	if _, err := exec.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("backfill user roles: %w", err)
	}
	return nil
}

func ensureColumn(ctx context.Context, exec executor, dialect Dialect, table string, column string, definition string) error {
	exists, err := columnExists(ctx, exec, dialect, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	quotedTable, quotedColumn, err := quotedIdentifiers(dialect, table, column)
	if err != nil {
		return err
	}
	if _, err = exec.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quotedTable, quotedColumn, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func columnExists(ctx context.Context, exec executor, dialect Dialect, table string, column string) (bool, error) {
	switch dialect {
	case DialectSQLite:
		rows, err := exec.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
		if err != nil {
			return false, fmt.Errorf("query SQLite columns: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, dataType string
			var notNull, primaryKey int
			var defaultValue any
			if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				return false, fmt.Errorf("scan SQLite column: %w", err)
			}
			if strings.EqualFold(name, column) {
				return true, nil
			}
		}
		return false, rows.Err()
	case DialectMySQL:
		var count int
		err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, column).Scan(&count)
		return count > 0, err
	case DialectPostgres:
		var count int
		err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2", table, column).Scan(&count)
		return count > 0, err
	default:
		return false, fmt.Errorf("unsupported migration dialect %q", dialect)
	}
}

func quotedIdentifiers(dialect Dialect, table string, column string) (string, string, error) {
	if strings.ContainsAny(table+column, "`\".';\\/ ") {
		return "", "", fmt.Errorf("invalid migration identifier")
	}
	switch dialect {
	case DialectMySQL:
		return "`" + table + "`", "`" + column + "`", nil
	case DialectSQLite, DialectPostgres:
		return `"` + table + `"`, `"` + column + `"`, nil
	default:
		return "", "", fmt.Errorf("unsupported migration dialect %q", dialect)
	}
}

func credentialStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "security_question" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL UNIQUE, "question" TEXT NOT NULL, "answer_hash" TEXT NOT NULL, "version" INTEGER NOT NULL DEFAULT 1, "created_at" DATETIME NOT NULL, "updated_at" DATETIME NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS "recovery_code" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "code_hash" TEXT NOT NULL, "code_version" INTEGER NOT NULL DEFAULT 1, "used_at" DATETIME NULL, "created_at" DATETIME NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_recovery_code_user_id" ON "recovery_code"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_recovery_code_used_at" ON "recovery_code"("used_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `security_question` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `question` VARCHAR(255) NOT NULL, `answer_hash` VARCHAR(255) NOT NULL, `version` INT NOT NULL DEFAULT 1, `created_at` DATETIME(6) NOT NULL, `updated_at` DATETIME(6) NOT NULL, UNIQUE KEY `uq_security_question_user_id` (`user_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `recovery_code` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `code_hash` VARCHAR(255) NOT NULL, `code_version` INT NOT NULL DEFAULT 1, `used_at` DATETIME(6) NULL, `created_at` DATETIME(6) NOT NULL, KEY `idx_recovery_code_user_id` (`user_id`), KEY `idx_recovery_code_used_at` (`used_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "security_question" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL UNIQUE, "question" VARCHAR(255) NOT NULL, "answer_hash" VARCHAR(255) NOT NULL, "version" INTEGER NOT NULL DEFAULT 1, "created_at" TIMESTAMPTZ NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS "recovery_code" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "code_hash" VARCHAR(255) NOT NULL, "code_version" INTEGER NOT NULL DEFAULT 1, "used_at" TIMESTAMPTZ NULL, "created_at" TIMESTAMPTZ NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_recovery_code_user_id" ON "recovery_code"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_recovery_code_used_at" ON "recovery_code"("used_at")`,
		},
	}
}

func webAuthnCredentialStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "webauthn_credential" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "credential_id" TEXT NOT NULL UNIQUE, "credential_data" TEXT NOT NULL, "display_name" TEXT NOT NULL, "created_at" DATETIME NOT NULL, "last_used_at" DATETIME NULL, "revoked_at" DATETIME NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_webauthn_credential_user_id" ON "webauthn_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_webauthn_credential_revoked_at" ON "webauthn_credential"("revoked_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `webauthn_credential` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `credential_id` VARCHAR(512) NOT NULL UNIQUE, `credential_data` TEXT NOT NULL, `display_name` VARCHAR(128) NOT NULL, `created_at` DATETIME(6) NOT NULL, `last_used_at` DATETIME(6) NULL, `revoked_at` DATETIME(6) NULL, KEY `idx_webauthn_credential_user_id` (`user_id`), KEY `idx_webauthn_credential_revoked_at` (`revoked_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "webauthn_credential" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "credential_id" VARCHAR(512) NOT NULL UNIQUE, "credential_data" TEXT NOT NULL, "display_name" VARCHAR(128) NOT NULL, "created_at" TIMESTAMPTZ NOT NULL, "last_used_at" TIMESTAMPTZ NULL, "revoked_at" TIMESTAMPTZ NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_webauthn_credential_user_id" ON "webauthn_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_webauthn_credential_revoked_at" ON "webauthn_credential"("revoked_at")`,
		},
	}
}
