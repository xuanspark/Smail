package migration

func userContactStatements() map[Dialect][]string {
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS "user_contact" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email" TEXT NOT NULL, "name" TEXT NOT NULL DEFAULT '', "last_used_at" DATETIME NOT NULL, UNIQUE("user_id", "email"))`,
			`CREATE INDEX IF NOT EXISTS "idx_user_contact_user_last_used" ON "user_contact"("user_id", "last_used_at")`,
			`CREATE TABLE IF NOT EXISTS "user_trusted_sender" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL, "email" TEXT NOT NULL, "created_at" DATETIME NOT NULL, UNIQUE("user_id", "email"))`,
			`CREATE INDEX IF NOT EXISTS "idx_user_trusted_sender_user" ON "user_trusted_sender"("user_id", "email")`,
		},
		DialectMySQL: {
			"CREATE TABLE IF NOT EXISTS `user_contact` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `email` VARCHAR(255) NOT NULL, `name` VARCHAR(100) NOT NULL DEFAULT '', `last_used_at` DATETIME(6) NOT NULL, UNIQUE KEY `uq_user_contact_user_email` (`user_id`, `email`), KEY `idx_user_contact_user_last_used` (`user_id`, `last_used_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE IF NOT EXISTS `user_trusted_sender` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, `user_id` BIGINT NOT NULL, `email` VARCHAR(255) NOT NULL, `created_at` DATETIME(6) NOT NULL, UNIQUE KEY `uq_user_trusted_sender_user_email` (`user_id`, `email`), KEY `idx_user_trusted_sender_user` (`user_id`, `email`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS "user_contact" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "email" VARCHAR(255) NOT NULL, "name" VARCHAR(100) NOT NULL DEFAULT '', "last_used_at" TIMESTAMPTZ NOT NULL, UNIQUE("user_id", "email"))`,
			`CREATE INDEX IF NOT EXISTS "idx_user_contact_user_last_used" ON "user_contact"("user_id", "last_used_at")`,
			`CREATE TABLE IF NOT EXISTS "user_trusted_sender" ("id" BIGSERIAL PRIMARY KEY, "user_id" BIGINT NOT NULL, "email" VARCHAR(255) NOT NULL, "created_at" TIMESTAMPTZ NOT NULL, UNIQUE("user_id", "email"))`,
			`CREATE INDEX IF NOT EXISTS "idx_user_trusted_sender_user" ON "user_trusted_sender"("user_id", "email")`,
		},
	}
}
