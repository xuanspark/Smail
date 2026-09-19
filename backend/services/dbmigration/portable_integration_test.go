package dbmigration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

// 集成测试只创建随机命名的新库，不修改现有业务库，也不自动删除测试库。
// 必须显式提供专用测试服务连接；缺少连接时跳过，不冒充实库验收成功。
func TestPortableSQLIntegration(t *testing.T) {
	for _, item := range []struct {
		kind config.DatabaseType
		env  string
	}{
		{config.DatabaseTypeMySQL, "SMAIL_TEST_MIGRATION_MYSQL"},
		{config.DatabaseTypePostgreSQL, "SMAIL_TEST_MIGRATION_POSTGRES"},
	} {
		t.Run(string(item.kind), func(t *testing.T) {
			input := portableIntegrationInput(t, item.env, item.kind)
			runPortableIntegrationChain(t, []TargetInput{input, input})
		})
	}
	t.Run("mysql_postgresql", func(t *testing.T) {
		mysql := portableIntegrationInput(t, "SMAIL_TEST_MIGRATION_MYSQL", config.DatabaseTypeMySQL)
		postgres := portableIntegrationInput(t, "SMAIL_TEST_MIGRATION_POSTGRES", config.DatabaseTypePostgreSQL)
		runPortableIntegrationChain(t, []TargetInput{mysql, postgres, mysql})
	})
}

func portableIntegrationInput(t *testing.T, name string, kind config.DatabaseType) TargetInput {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		t.Skip(name + " is not configured")
	}
	var input TargetInput
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Type != kind || input.Name != "" || input.LocalRef != "" {
		t.Fatal("integration connection must specify the expected type without a database name or local path")
	}
	return input
}

func runPortableIntegrationChain(t *testing.T, inputs []TargetInput) {
	t.Helper()
	root, source := portableFixture(t)
	open, err := NewRuntimeSQLConnectionFactory(root)
	if err != nil {
		t.Fatal("integration connection factory unavailable")
	}
	inputs = append(inputs, TargetInput{Type: config.DatabaseTypeSQLite, LocalRef: "roundtrip.db"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for index, input := range inputs {
		if input.Type != config.DatabaseTypeSQLite {
			input.Name, err = randomID("smail_migration_test_")
			if err != nil {
				t.Fatal("integration name generation failed")
			}
		}
		target, err := input.Prepare(root)
		if err != nil {
			t.Fatal("integration connection configuration is invalid")
		}
		plan, err := buildTargetPlan(source, target)
		if err != nil {
			t.Fatal("integration plan validation failed")
		}
		switched := false
		executor := &PortableExecutor{root: root, open: open, switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
			switched = true
			return nil
		}}
		// 路径只记录随机测试库名，不记录连接账号、密码或底层驱动错误。
		t.Logf("hop %d: %s -> %s, retained database: %s%s", index+1, source.Type, target.Type, target.Name, target.LocalRef)
		if err := executor.Run(ctx, plan, func(Status) error { return nil }); err != nil {
			executor.Close()
			t.Fatalf("migration failed: %s", migrationErrorCode(err))
		}
		executor.Close()
		if !switched {
			t.Fatal("configuration switch was not reached")
		}
		connection, err := executor.connect(ctx, target)
		if err != nil {
			t.Fatal("migrated target could not be reopened")
		}
		var subject string
		queryErr := connection.QueryRowContext(ctx, "SELECT subject FROM email WHERE id=82").Scan(&subject)
		var logPayload string
		logErr := connection.QueryRowContext(ctx, "SELECT payload FROM mail_log WHERE id='migration-log'").Scan(&logPayload)
		_, insertErr := connection.ExecContext(ctx, "INSERT INTO "+quoteDatabaseIdentifier(target.Type, "user")+" (account,name,password) VALUES ('next_hop_"+string(rune('a'+index))+"','sequence','test-only')")
		_ = connection.Close()
		if queryErr != nil || subject != "中文主题" || logErr != nil || logPayload != `{"message":"中文日志"}` || insertErr != nil {
			t.Fatal("target content or identity sequence was not preserved")
		}
		source = target
	}
}
