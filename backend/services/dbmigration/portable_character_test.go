package dbmigration

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

// 用真实行扫描接口重放云端驱动类型；这些用例不能代替双 SQL 实机验收。
func TestPortableFixedCharacterDriverValues(t *testing.T) {
	for _, item := range []struct {
		name        string
		kind        config.DatabaseType
		columnType  string
		input, want any
	}{
		{"postgres_empty", config.DatabaseTypePostgreSQL, "BPCHAR", []byte("    "), []byte("")},
		{"postgres_token", config.DatabaseTypePostgreSQL, "BPCHAR", []byte("abc   "), []byte("abc")},
		{"postgres_string", config.DatabaseTypePostgreSQL, "BPCHAR", "abc   ", "abc"},
		{"postgres_null", config.DatabaseTypePostgreSQL, "BPCHAR", nil, nil},
		{"mysql_padding", config.DatabaseTypeMySQL, "CHAR", []byte("abc   "), []byte("abc")},
		{"postgres_leading_space", config.DatabaseTypePostgreSQL, "BPCHAR", []byte(" abc  "), []byte(" abc")},
		{"postgres_other_whitespace", config.DatabaseTypePostgreSQL, "BPCHAR", []byte("abc\t\n\u00a0  "), []byte("abc\t\n\u00a0")},
		{"postgres_varchar", config.DatabaseTypePostgreSQL, "VARCHAR", "abc  ", "abc  "},
		{"mysql_varchar", config.DatabaseTypeMySQL, "VARCHAR", []byte("abc  "), []byte("abc  ")},
		{"postgres_text", config.DatabaseTypePostgreSQL, "TEXT", "abc  ", "abc  "},
		{"postgres_binary", config.DatabaseTypePostgreSQL, "BYTEA", []byte{0, 255, 32, 32}, []byte{0, 255, 32, 32}},
		{"mysql_binary", config.DatabaseTypeMySQL, "BLOB", []byte{0, 255, 32}, []byte{0, 255, 32}},
		{"sqlite_char", config.DatabaseTypeSQLite, "CHAR", "abc  ", "abc  "},
		{"sqlite_bpchar", config.DatabaseTypeSQLite, "BPCHAR", []byte("abc  "), []byte("abc  ")},
	} {
		t.Run(item.name, func(t *testing.T) {
			database := portableCharacterFixture(t, item.columnType, item.input)
			count := 0
			err := visitPortableRows(context.Background(), database, item.kind, "sample", []string{"value"}, func(values []any) error {
				count++
				if !reflect.DeepEqual(values[0], item.want) {
					t.Fatal("定长字符填充或非定长数据处理错误")
				}
				return nil
			})
			if err != nil || count != 1 {
				t.Fatal("驱动类型重放未完成")
			}
		})
	}
}

func TestPortableFixedCharacterDigest(t *testing.T) {
	for _, item := range []struct {
		name                   string
		sourceType, targetType string
		source, target         any
		equal                  bool
	}{
		{"empty_padding", "CHAR", "BPCHAR", []byte(""), []byte("    "), true},
		{"token_padding", "CHAR", "BPCHAR", []byte("abc"), []byte("abc    "), true},
		{"changed_token", "CHAR", "BPCHAR", []byte("abc"), []byte("abd    "), false},
		{"leading_space", "CHAR", "BPCHAR", []byte("abc"), []byte(" abc   "), false},
		{"null_not_empty", "CHAR", "BPCHAR", nil, []byte("    "), false},
		{"varchar_space", "VARCHAR", "VARCHAR", []byte("abc"), "abc ", false},
		{"text_space", "TEXT", "TEXT", []byte("abc"), "abc ", false},
		{"binary_space", "BLOB", "BYTEA", []byte{0, 255}, []byte{0, 255, 32}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			source := portableCharacterFixture(t, item.sourceType, item.source)
			target := portableCharacterFixture(t, item.targetType, item.target)
			types := map[string]string{"value": item.targetType}
			left, err := digestPortableTable(context.Background(), source, config.DatabaseTypeMySQL, "sample", []string{"value"}, types, config.DatabaseTypePostgreSQL)
			if err != nil {
				t.Fatal("源摘要计算失败")
			}
			right, err := digestPortableTable(context.Background(), target, config.DatabaseTypePostgreSQL, "sample", []string{"value"}, types, config.DatabaseTypePostgreSQL)
			if err != nil {
				t.Fatal("目标摘要计算失败")
			}
			if (left == right) != item.equal {
				t.Fatal("摘要未区分填充与真实数据差异")
			}
		})
	}
}

func portableCharacterFixture(t *testing.T, columnType string, value any) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal("创建重放数据库失败")
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	// 类型来自本文件固定用例，不接受外部输入。
	if _, err := database.Exec("CREATE TABLE sample (value " + columnType + ")"); err != nil {
		t.Fatal("创建重放表失败")
	}
	if _, err := database.Exec("INSERT INTO sample(value) VALUES (?)", value); err != nil {
		t.Fatal("写入重放值失败")
	}
	return database
}
