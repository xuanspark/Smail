package migration

func protocolCredentialStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "protocol_credential" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "protocol" TEXT NOT NULL, "enabled" INTEGER NOT NULL DEFAULT 0, "code_hash" TEXT NOT NULL DEFAULT '', "code_version" INTEGER NOT NULL DEFAULT 1, "created_at" DATETIME NOT NULL, "updated_at" DATETIME NOT NULL, "last_used_at" DATETIME NULL, "revoked_at" DATETIME NULL, UNIQUE("user_id", "protocol"))`,
			`CREATE INDEX IF NOT EXISTS "idx_protocol_credential_user_id" ON "protocol_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_protocol_credential_protocol_enabled" ON "protocol_credential"("protocol", "enabled", "revoked_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `protocol_credential` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `protocol` VARCHAR(16) NOT NULL, `enabled` TINYINT(1) NOT NULL DEFAULT 0, `code_hash` VARCHAR(255) NOT NULL DEFAULT '', `code_version` INT NOT NULL DEFAULT 1, `created_at` DATETIME(6) NOT NULL, `updated_at` DATETIME(6) NOT NULL, `last_used_at` DATETIME(6) NULL, `revoked_at` DATETIME(6) NULL, UNIQUE KEY `uq_protocol_credential_user_protocol` (`user_id`, `protocol`), KEY `idx_protocol_credential_user_id` (`user_id`), KEY `idx_protocol_credential_protocol_enabled` (`protocol`, `enabled`, `revoked_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "protocol_credential" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "protocol" VARCHAR(16) NOT NULL, "enabled" BOOLEAN NOT NULL DEFAULT FALSE, "code_hash" VARCHAR(255) NOT NULL DEFAULT '', "code_version" INTEGER NOT NULL DEFAULT 1, "created_at" TIMESTAMPTZ NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL, "last_used_at" TIMESTAMPTZ NULL, "revoked_at" TIMESTAMPTZ NULL, UNIQUE("user_id", "protocol"))`,
			`CREATE INDEX IF NOT EXISTS "idx_protocol_credential_user_id" ON "protocol_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_protocol_credential_protocol_enabled" ON "protocol_credential"("protocol", "enabled", "revoked_at")`,
		},
	}
}
