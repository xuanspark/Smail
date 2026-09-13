package migration

func clientCredentialStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "client_credential" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "client_name" TEXT NOT NULL, "code_hash" TEXT NOT NULL DEFAULT '', "code_ciphertext" TEXT NULL, "code_version" INTEGER NOT NULL DEFAULT 1, "created_at" DATETIME NOT NULL, "updated_at" DATETIME NOT NULL, "last_used_at" DATETIME NULL, "revoked_at" DATETIME NULL, UNIQUE("user_id", "client_name"))`,
			`CREATE INDEX IF NOT EXISTS "idx_client_credential_user_id" ON "client_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_client_credential_revoked" ON "client_credential"("revoked_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `client_credential` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `client_name` VARCHAR(64) NOT NULL, `code_hash` VARCHAR(255) NOT NULL DEFAULT '', `code_ciphertext` VARCHAR(512) NULL, `code_version` INT NOT NULL DEFAULT 1, `created_at` DATETIME(6) NOT NULL, `updated_at` DATETIME(6) NOT NULL, `last_used_at` DATETIME(6) NULL, `revoked_at` DATETIME(6) NULL, UNIQUE KEY `uq_client_credential_user_client` (`user_id`, `client_name`), KEY `idx_client_credential_user_id` (`user_id`), KEY `idx_client_credential_revoked` (`revoked_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "client_credential" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "client_name" VARCHAR(64) NOT NULL, "code_hash" VARCHAR(255) NOT NULL DEFAULT '', "code_ciphertext" VARCHAR(512) NULL, "code_version" INTEGER NOT NULL DEFAULT 1, "created_at" TIMESTAMPTZ NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL, "last_used_at" TIMESTAMPTZ NULL, "revoked_at" TIMESTAMPTZ NULL, UNIQUE("user_id", "client_name"))`,
			`CREATE INDEX IF NOT EXISTS "idx_client_credential_user_id" ON "client_credential"("user_id")`,
			`CREATE INDEX IF NOT EXISTS "idx_client_credential_revoked" ON "client_credential"("revoked_at")`,
		},
	}
}
