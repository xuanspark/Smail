package migration

func accountGroupStatements() map[Dialect][]string {
	members := `CREATE TABLE IF NOT EXISTS account_group_member (
		user_id BIGINT NOT NULL,
		group_id BIGINT NOT NULL,
		PRIMARY KEY (user_id, group_id)
	)`
	return map[Dialect][]string{
		DialectSQLite: {
			`CREATE TABLE IF NOT EXISTS account_group (id INTEGER PRIMARY KEY AUTOINCREMENT, name VARCHAR(64) NOT NULL UNIQUE)`,
			members,
			`CREATE INDEX IF NOT EXISTS "idx_account_group_members" ON account_group_member (group_id, user_id)`,
		},
		DialectMySQL: {
			`CREATE TABLE IF NOT EXISTS account_group (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, name VARCHAR(64) NOT NULL UNIQUE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
			`CREATE TABLE IF NOT EXISTS account_group_member (user_id BIGINT NOT NULL, group_id BIGINT NOT NULL, PRIMARY KEY (user_id, group_id), KEY idx_account_group_members (group_id, user_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		},
		DialectPostgres: {
			`CREATE TABLE IF NOT EXISTS account_group (id BIGSERIAL PRIMARY KEY, name VARCHAR(64) NOT NULL UNIQUE)`,
			members,
			`CREATE INDEX IF NOT EXISTS idx_account_group_members ON account_group_member (group_id, user_id)`,
		},
	}
}
