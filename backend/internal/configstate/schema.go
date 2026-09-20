package configstate

func Schema(driver string) string {
	switch driver {
	case "mysql":
		return "CREATE TABLE IF NOT EXISTS config_records (kind VARCHAR(16) NOT NULL, id VARCHAR(32) NOT NULL, payload LONGBLOB NOT NULL, digest VARCHAR(64) NOT NULL, PRIMARY KEY(kind,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin"
	case "postgres":
		return "CREATE TABLE IF NOT EXISTS config_records (kind VARCHAR(16) NOT NULL, id VARCHAR(32) NOT NULL, payload BYTEA NOT NULL, digest VARCHAR(64) NOT NULL, PRIMARY KEY(kind,id))"
	default:
		return "CREATE TABLE IF NOT EXISTS config_records (kind TEXT NOT NULL, id TEXT NOT NULL, payload BLOB NOT NULL, digest TEXT NOT NULL, PRIMARY KEY(kind,id))"
	}
}
