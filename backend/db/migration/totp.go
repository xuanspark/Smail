package migration

func totpAuthenticatorStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "totp_authenticator" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL UNIQUE, "secret_ciphertext" TEXT NOT NULL, "credential_version" INTEGER NOT NULL DEFAULT 1, "enabled" INTEGER NOT NULL DEFAULT 1, "confirmed_at" DATETIME NULL, "last_used_step" INTEGER NOT NULL DEFAULT 0, "created_at" DATETIME NOT NULL, "updated_at" DATETIME NOT NULL, "revoked_at" DATETIME NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_totp_authenticator_revoked_at" ON "totp_authenticator"("revoked_at")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `totp_authenticator` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `secret_ciphertext` TEXT NOT NULL, `credential_version` INT NOT NULL DEFAULT 1, `enabled` TINYINT(1) NOT NULL DEFAULT 1, `confirmed_at` DATETIME(6) NULL, `last_used_step` BIGINT NOT NULL DEFAULT 0, `created_at` DATETIME(6) NOT NULL, `updated_at` DATETIME(6) NOT NULL, `revoked_at` DATETIME(6) NULL, UNIQUE KEY `uq_totp_authenticator_user_id` (`user_id`), KEY `idx_totp_authenticator_revoked_at` (`revoked_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "totp_authenticator" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL UNIQUE, "secret_ciphertext" TEXT NOT NULL, "credential_version" INTEGER NOT NULL DEFAULT 1, "enabled" BOOLEAN NOT NULL DEFAULT TRUE, "confirmed_at" TIMESTAMPTZ NULL, "last_used_step" BIGINT NOT NULL DEFAULT 0, "created_at" TIMESTAMPTZ NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL, "revoked_at" TIMESTAMPTZ NULL)`,
			`CREATE INDEX IF NOT EXISTS "idx_totp_authenticator_revoked_at" ON "totp_authenticator"("revoked_at")`,
		},
	}
}
