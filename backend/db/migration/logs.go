package migration

// 日志使用可移植的数值时间和文本载荷，不依赖数据库专属 JSON、分区或事件调度器。
func logStatements() map[Dialect][]string {
	result := make(map[Dialect][]string)
	for _, dialect := range []Dialect{DialectSQLite, DialectMySQL, DialectPostgres} {
		for _, table := range []string{"operation_log", "mail_log"} {
			payloadType := "TEXT"
			if dialect == DialectMySQL {
				payloadType = "LONGTEXT"
			}
			statement := "CREATE TABLE IF NOT EXISTS " + table + " (id VARCHAR(80) PRIMARY KEY, created_at BIGINT NOT NULL, kind VARCHAR(32) NOT NULL, actor_id VARCHAR(128) NOT NULL, action VARCHAR(64) NOT NULL, section VARCHAR(32) NOT NULL, result VARCHAR(16) NOT NULL, payload " + payloadType + " NOT NULL"
			if dialect == DialectMySQL {
				statement += ", KEY idx_" + table + "_time (created_at, id), KEY idx_" + table + "_kind_time (kind, created_at, id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
			} else {
				statement += ")"
			}
			result[dialect] = append(result[dialect], statement)
			if dialect != DialectMySQL {
				result[dialect] = append(result[dialect],
					"CREATE INDEX IF NOT EXISTS \"idx_"+table+"_time\" ON "+table+" (created_at, id)",
					"CREATE INDEX IF NOT EXISTS \"idx_"+table+"_kind_time\" ON "+table+" (kind, created_at, id)")
			}
		}
	}
	return result
}
