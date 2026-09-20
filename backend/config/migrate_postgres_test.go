package config

import "testing"

func TestLegacyPostgresRuntimeOptions(t *testing.T) {
	for _, test := range []struct {
		query string
		valid bool
	}{
		{"", true},
		{"sslmode=require", true},
		{"sslmode=require&timezone=UTC&search_path=public", true},
		{"sslmode=disable", false},
		{"sslmode=prefer", false},
		{"sslmode=verify-full", false},
		{"sslmode=require&sslmode=disable", false},
		{"sslmode=require&sslmode=require", false},
		{"sslmode=require&options=-c%20search_path%3Dprivate", false},
		{"sslmode=require&search_path=private", false},
		{"sslmode=require&timezone=Asia%2FShanghai", false},
		{"sslmode=require&unknown=", false},
		{"sslmode=require;search_path=private", false},
		{"sslmode=%zz", false},
	} {
		t.Run(test.query, func(t *testing.T) {
			value := "postgres://smail:sample@127.0.0.1:5432/smail"
			if test.query != "" {
				value += "?" + test.query
			}
			database, password, valid := parseLegacyPostgreSQLDSN(value)
			if valid != test.valid {
				t.Fatalf("连接选项验证错误：%t", valid)
			}
			if valid && (database.Type != DatabaseTypePostgreSQL || database.Port != 5432 || password != "sample") {
				t.Fatal("安全选项影响了数据库连接字段")
			}
		})
	}
}

func TestLegacyPostgresSetupDSNMigration(t *testing.T) {
	root := t.TempDir()
	writeLegacyMigrationFixture(t, root, map[string]any{
		"dbType": DBTypePostgres,
		"dbDSN":  "postgres://smail:sample@127.0.0.1:5432/smail?sslmode=require",
	})
	plan, err := PlanLegacyMigration(root, RuntimeValidationOptions{})
	if err != nil || len(plan.ManualFields) != 0 {
		t.Fatalf("首次安装产生的安全连接串不能迁移：%v", err)
	}
}
