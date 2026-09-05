package dbmigration

import (
	"context"

	"git.pcux.cn/xuans/Smail/config"
)

// 仅迁移 Smail 版本化业务表；不能静默忽略自定义对象。
func validatePortableObjects(ctx context.Context, database portableQuery, kind config.DatabaseType) error {
	queries := []string{"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('view','trigger')"}
	if kind == config.DatabaseTypeSQLite {
		queries = append(queries, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND sql IS NOT NULL")
	}
	switch kind {
	case config.DatabaseTypeMySQL:
		queries = []string{
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND (table_type <> 'BASE TABLE' OR engine <> 'InnoDB')",
			"SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE()",
			"SELECT COUNT(*) FROM information_schema.events WHERE event_schema=DATABASE()",
			"SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema=DATABASE()",
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema=DATABASE() AND constraint_type='FOREIGN KEY'",
		}
	case config.DatabaseTypePostgreSQL:
		queries = []string{
			"SELECT COUNT(*) FROM pg_namespace WHERE nspname NOT IN ('public','information_schema') AND nspname NOT LIKE 'pg_%'",
			"SELECT COUNT(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind NOT IN ('r','i','S')",
			"SELECT COUNT(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal",
			"SELECT COUNT(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'",
			"SELECT COUNT(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public' AND c.contype IN ('f','c','x')",
		}
	}
	for _, query := range queries {
		rows, err := database.QueryContext(ctx, query)
		if err != nil {
			return ErrSQLUnsupportedObject
		}
		var count int
		valid := rows.Next() && rows.Scan(&count) == nil && count == 0 && rows.Err() == nil
		_ = rows.Close()
		if !valid {
			return ErrSQLUnsupportedObject
		}
	}
	return nil
}
