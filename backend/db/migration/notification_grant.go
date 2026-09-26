package migration

import "context"

func applyNotificationCiphertextSchema(ctx context.Context, exec executor, dialect Dialect) error {
	return ensureColumn(ctx, exec, dialect, "notification_grant", "code_ciphertext", "VARCHAR(512) NULL")
}

func notificationGrantStatements() map[Dialect][]string {
	// 不依赖自增、布尔或日期方言；计数窗口统一使用 UTC 字符串。
	statement := `CREATE TABLE IF NOT EXISTS notification_grant (
		user_id BIGINT NOT NULL PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		code_version INTEGER NOT NULL DEFAULT 1,
		code_hash VARCHAR(64) NOT NULL DEFAULT '',
		session_version INTEGER NOT NULL DEFAULT 0,
		security_version INTEGER NOT NULL DEFAULT 0,
		minute_limit INTEGER NOT NULL DEFAULT 10,
		day_limit INTEGER NOT NULL DEFAULT 100,
		minute_key VARCHAR(16) NOT NULL DEFAULT '',
		minute_count INTEGER NOT NULL DEFAULT 0,
		day_key VARCHAR(10) NOT NULL DEFAULT '',
		day_count INTEGER NOT NULL DEFAULT 0
	)`
	return map[Dialect][]string{
		DialectSQLite:   {statement},
		DialectMySQL:    {statement + " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"},
		DialectPostgres: {statement},
	}
}
