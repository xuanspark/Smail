package migration

import "context"

func applyOutboundSubmissionSchema(ctx context.Context, exec executor, dialect Dialect) error {
	return ensureColumn(ctx, exec, dialect, "outbound_delivery", "submission", "TEXT NULL")
}

func outboundDeliveryStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "outbound_delivery" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "email_id" INTEGER NOT NULL, "recipient" TEXT NOT NULL, "state" TEXT NOT NULL, "attempts" INTEGER NOT NULL DEFAULT 0, "next_attempt" BIGINT NOT NULL DEFAULT 0, "last_error" TEXT NOT NULL DEFAULT '')`,
			`CREATE INDEX IF NOT EXISTS "idx_outbound_delivery_email" ON "outbound_delivery"("email_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_outbound_delivery_due" ON "outbound_delivery"("state", "next_attempt")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `outbound_delivery` (`id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, `email_id` BIGINT NOT NULL, `recipient` VARCHAR(320) NOT NULL, `state` VARCHAR(24) NOT NULL, `attempts` INT NOT NULL DEFAULT 0, `next_attempt` BIGINT NOT NULL DEFAULT 0, `last_error` TEXT NOT NULL, KEY `idx_outbound_delivery_email` (`email_id`), KEY `idx_outbound_delivery_due` (`state`, `next_attempt`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "outbound_delivery" ("id" BIGSERIAL PRIMARY KEY, "email_id" BIGINT NOT NULL, "recipient" VARCHAR(320) NOT NULL, "state" VARCHAR(24) NOT NULL, "attempts" INTEGER NOT NULL DEFAULT 0, "next_attempt" BIGINT NOT NULL DEFAULT 0, "last_error" TEXT NOT NULL DEFAULT '')`,
			`CREATE INDEX IF NOT EXISTS "idx_outbound_delivery_email" ON "outbound_delivery"("email_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_outbound_delivery_due" ON "outbound_delivery"("state", "next_attempt")`,
		},
	}
}
