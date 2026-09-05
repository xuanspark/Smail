package dbmigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
)

type portableQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func portableTables(ctx context.Context, database portableQuery, kind config.DatabaseType) ([]string, error) {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
	if kind == config.DatabaseTypeMySQL {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE' ORDER BY table_name"
	}
	if kind == config.DatabaseTypePostgreSQL {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name"
	}
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, ErrSQLConnectionUnavailable
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, ErrSQLUnsupportedObject
		}
		tables = append(tables, name)
	}
	if rows.Err() != nil || len(tables) == 0 {
		return nil, ErrSQLUnsupportedObject
	}
	return tables, nil
}

func portableColumns(ctx context.Context, database portableQuery, kind config.DatabaseType, table string) ([]string, map[string]string, error) {
	rows, err := database.QueryContext(ctx, "SELECT * FROM "+quoteDatabaseIdentifier(kind, table)+" WHERE 1=0")
	if err != nil {
		return nil, nil, ErrSQLUnsupportedObject
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	result := make(map[string]string, len(types))
	for _, column := range types {
		result[column.Name()] = strings.ToUpper(column.DatabaseTypeName())
	}
	sort.Strings(columns)
	return columns, result, nil
}

func validatePortableSchema(ctx context.Context, source portableQuery, target *sql.DB, plan Plan, tables []string) error {
	targetTables, err := portableTables(ctx, target, plan.Target.Type)
	if err != nil || !sameStringSet(tables, targetTables) {
		return ErrSQLUnsupportedObject
	}
	rows, err := source.QueryContext(ctx, "SELECT version, checksum FROM schema_migration ORDER BY version")
	if err != nil {
		return ErrSQLUnsupportedObject
	}
	defer rows.Close()
	definitions := migration.DefaultMigrations()
	index := 0
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil || index >= len(definitions) || definitions[index].Version != version || !definitions[index].AcceptsChecksum(checksum) {
			return ErrSQLUnsupportedObject
		}
		index++
	}
	if rows.Err() != nil || index != len(definitions) {
		return ErrSQLUnsupportedObject
	}
	return nil
}

func copyPortableTables(ctx context.Context, source *sql.Conn, target *sql.DB, plan Plan, tables []string) error {
	if err := validatePortableSchema(ctx, source, target, plan, tables); err != nil {
		return err
	}
	transaction, err := target.BeginTx(ctx, nil)
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer transaction.Rollback()
	for _, table := range tables {
		if table == "schema_migration" {
			continue
		}
		columns, _, err := portableColumns(ctx, source, plan.Source.Type, table)
		if err != nil {
			return err
		}
		targetColumns, types, err := portableColumns(ctx, transaction, plan.Target.Type, table)
		if err != nil || !sameStringSet(columns, targetColumns) {
			return ErrSQLUnsupportedObject
		}
		query := "INSERT INTO " + quoteDatabaseIdentifier(plan.Target.Type, table) + " (" + portableColumnList(plan.Target.Type, columns) + ") VALUES ("
		placeholders := make([]string, len(columns))
		for i := range columns {
			placeholders[i] = "?"
			if plan.Target.Type == config.DatabaseTypePostgreSQL {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			}
		}
		statement, err := transaction.PrepareContext(ctx, query+strings.Join(placeholders, ",")+")")
		if err != nil {
			return ErrSQLUnsupportedObject
		}
		err = visitPortableRows(ctx, source, plan.Source.Type, table, columns, func(values []any) error {
			for i, value := range values {
				converted, err := convertPortableValue(value, table, columns[i], types[columns[i]], plan.Target.Type)
				if err != nil {
					return err
				}
				values[i] = converted
			}
			if _, err := statement.ExecContext(ctx, values...); err != nil {
				return ErrSQLDataVerification
			}
			return nil
		})
		_ = statement.Close()
		if err != nil {
			return err
		}
		if plan.Target.Type == config.DatabaseTypePostgreSQL && types["id"] != "" {
			// 显式复制主键后必须推进序列，否则重启后的新增记录会冲突。
			quoted := quotePostgreSQLIdentifier(table)
			query := "SELECT setval(pg_get_serial_sequence($1, 'id'), COALESCE(MAX(id), 1), MAX(id) IS NOT NULL) FROM " + quoted
			if _, err := transaction.ExecContext(ctx, query, quoted); err != nil {
				return ErrSQLDataVerification
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return ErrSQLDataVerification
	}
	return nil
}

func portableColumnList(kind config.DatabaseType, columns []string) string {
	result := make([]string, len(columns))
	for i, column := range columns {
		result[i] = quoteDatabaseIdentifier(kind, column)
	}
	return strings.Join(result, ",")
}

func visitPortableRows(ctx context.Context, database portableQuery, kind config.DatabaseType, table string, columns []string, visit func([]any) error) error {
	rows, err := database.QueryContext(ctx, "SELECT "+portableColumnList(kind, columns)+" FROM "+quoteDatabaseIdentifier(kind, table))
	if err != nil {
		return ErrSQLConnectionUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return ErrSQLDataVerification
		}
		if err := visit(values); err != nil {
			return err
		}
	}
	if rows.Err() != nil {
		return ErrSQLDataVerification
	}
	return nil
}

func convertPortableValue(value any, table, column, targetType string, target config.DatabaseType) (any, error) {
	if value == nil {
		return nil, nil
	}
	if table == "sessions" && column == "expiry" {
		var instant time.Time
		switch typed := value.(type) {
		case time.Time:
			instant = typed
		case float64:
			// sqlite3store 实际保存儒略日，而非 Unix 秒。
			seconds, fraction := math.Modf((typed - 2440587.5) * 86400)
			instant = time.Unix(int64(seconds), int64(fraction*1e9)).UTC()
		case int64:
			return nil, ErrSQLDataVerification
		default:
			return nil, ErrSQLDataVerification
		}
		if target == config.DatabaseTypeSQLite {
			if original, ok := value.(float64); ok {
				return original, nil
			}
			return 2440587.5 + (float64(instant.Unix())+float64(instant.Nanosecond())/1e9)/86400, nil
		}
		return instant.UTC().Truncate(time.Microsecond), nil
	}
	if bytes, ok := value.([]byte); ok && !strings.Contains(targetType, "BLOB") && targetType != "BYTEA" {
		value = string(bytes)
	}
	if targetType == "BOOL" || targetType == "BOOLEAN" {
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case int64:
			if typed == 0 || typed == 1 {
				return typed == 1, nil
			}
		default:
			return nil, ErrSQLDataVerification
		}
		return nil, ErrSQLDataVerification
	}
	if boolean, ok := value.(bool); ok {
		if boolean {
			return int64(1), nil
		}
		return int64(0), nil
	}
	if instant, ok := value.(time.Time); ok {
		return instant.UTC().Truncate(time.Microsecond), nil
	}
	if text, ok := value.(string); ok && (strings.Contains(targetType, "TIME") || targetType == "DATE") {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
			if instant, err := time.Parse(layout, text); err == nil {
				return instant.UTC().Truncate(time.Microsecond), nil
			}
		}
		return nil, ErrSQLDataVerification
	}
	return value, nil
}

type portableDigest struct {
	Count uint64
	Sum   [32]byte
}

func digestPortableTable(ctx context.Context, database portableQuery, kind config.DatabaseType, table string, columns []string, types map[string]string, target config.DatabaseType) (portableDigest, error) {
	var digest portableDigest
	err := visitPortableRows(ctx, database, kind, table, columns, func(values []any) error {
		hash := sha256.New()
		for i, value := range values {
			value, err := convertPortableValue(value, table, columns[i], types[columns[i]], target)
			if err != nil {
				return err
			}
			var text string
			switch typed := value.(type) {
			case nil:
				_, _ = hash.Write([]byte{0})
				continue
			case time.Time:
				text = typed.UTC().Format(time.RFC3339Nano)
			case []byte:
				text = string(typed)
			case bool:
				text = strconv.FormatBool(typed)
			case float64:
				// 儒略日单位为天，六位小数会掩盖约八十六毫秒的差异。
				text = strconv.FormatFloat(typed, 'g', -1, 64)
			default:
				text = fmt.Sprint(typed)
			}
			var length [8]byte
			_, _ = hash.Write([]byte{1})
			binary.BigEndian.PutUint64(length[:], uint64(len(text)))
			_, _ = hash.Write(length[:])
			_, _ = hash.Write([]byte(text))
		}
		row := hash.Sum(nil)
		carry := uint16(0)
		for i := 31; i >= 0; i-- {
			carry += uint16(digest.Sum[i]) + uint16(row[i])
			digest.Sum[i] = byte(carry)
			carry >>= 8
		}
		digest.Count++
		return nil
	})
	return digest, err
}

func verifyPortableTables(ctx context.Context, source *sql.Conn, target *sql.DB, plan Plan, tables []string) error {
	for _, table := range tables {
		if table == "schema_migration" {
			continue
		}
		columns, types, err := portableColumns(ctx, target, plan.Target.Type, table)
		if err != nil {
			return err
		}
		before, err := digestPortableTable(ctx, source, plan.Source.Type, table, columns, types, plan.Target.Type)
		if err != nil {
			return err
		}
		after, err := digestPortableTable(ctx, target, plan.Target.Type, table, columns, types, plan.Target.Type)
		if err != nil {
			return err
		}
		if before != after {
			return ErrSQLDataVerification
		}
	}
	return nil
}
