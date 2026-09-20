package dbmigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

func TestIdentifierQuotationContainsUntrustedSyntax(t *testing.T) {
	for _, value := range []string{
		"ordinary", "with space", "mail`prod", `mail"prod`,
		"quote`; DROP TABLE sentinel; --", `quote"; DROP TABLE sentinel; --`,
		"comma,other", "comment--suffix", "中文表名", "back\\slash",
	} {
		for _, kind := range []config.DatabaseType{config.DatabaseTypeMySQL, config.DatabaseTypePostgreSQL} {
			t.Run(string(kind)+"/"+value, func(t *testing.T) {
				quoted := quoteDatabaseIdentifier(kind, value)
				delimiter := byte('"')
				if kind == config.DatabaseTypeMySQL {
					delimiter = '`'
				}
				if len(quoted) < 2 || quoted[0] != delimiter || quoted[len(quoted)-1] != delimiter {
					t.Fatal("标识符缺少完整引用边界")
				}
				var decoded strings.Builder
				for index := 1; index < len(quoted)-1; index++ {
					if quoted[index] == delimiter {
						index++
						if index >= len(quoted)-1 || quoted[index] != delimiter {
							t.Fatal("标识符内部存在未成对的结束引用")
						}
					}
					decoded.WriteByte(quoted[index])
				}
				if decoded.String() != value {
					t.Fatal("标识符引用改变了原始名称")
				}
			})
		}
	}
}

func TestMySQLReferenceRulesRejectInjectedSuffixes(t *testing.T) {
	for _, rule := range []string{"CASCADE", "SET NULL", "SET DEFAULT", "RESTRICT", "NO ACTION"} {
		for _, suffix := range []string{"; SELECT 1", " --", "/*comment*/", "\x00", ", CASCADE"} {
			if validMySQLReferenceRule(rule + suffix) {
				t.Fatal("外键动作接受了白名单以外的语法")
			}
		}
	}
}

// 只接受新建的隔离数据库；保留表和测试数据，不连接正式数据库或删除文件。
func TestSQLIdentifierLiveIntegration(t *testing.T) {
	for _, kind := range []config.DatabaseType{config.DatabaseTypeMySQL, config.DatabaseTypePostgreSQL} {
		t.Run(string(kind), func(t *testing.T) {
			raw := os.Getenv("SMAIL_TEST_IDENTIFIER_" + strings.ToUpper(string(kind)))
			if raw == "" {
				t.Skip("未配置专用实机标识符测试数据库")
			}
			var input struct {
				Name     string `json:"name"`
				User     string `json:"user"`
				Password string `json:"password"`
			}
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil ||
				!regexp.MustCompile(`^smail_runtime_test_[0-9a-f]{12}$`).MatchString(input.Name) ||
				!regexp.MustCompile(`^smail_rt_[0-9a-f]{12}$`).MatchString(input.User) || input.Password == "" {
				t.Fatal("实机测试连接必须使用专用隔离数据库和账号")
			}
			port := uint16(3306)
			if kind == config.DatabaseTypePostgreSQL {
				port = 5432
			}
			settings := config.DatabaseConfig{Type: kind, Host: "127.0.0.1", Port: port, Username: input.User}
			driver, dsn, err := runtimeSQLDSN(settings, input.Password, input.Name)
			if err != nil {
				t.Fatal("无法构建隔离连接")
			}
			database, err := sql.Open(driver, dsn)
			if err != nil {
				t.Fatal("无法打开隔离连接")
			}
			defer database.Close()
			database.SetMaxOpenConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			var count int
			query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()"
			if kind == config.DatabaseTypePostgreSQL {
				query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public'"
			}
			if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
				var pathError *os.PathError
				if errors.As(err, &pathError) {
					t.Fatalf("测试连接的文件访问失败：操作=%s 文件=%s", pathError.Op, filepath.Base(pathError.Path))
				}
				t.Fatalf("测试库查询失败，错误类型：%T", err)
			}
			if count != 0 {
				t.Fatal("测试数据库必须为空，拒绝复用现有业务数据")
			}
			if _, err := database.ExecContext(ctx, "CREATE TABLE sentinel (id INTEGER PRIMARY KEY)"); err != nil {
				t.Fatal("隔离哨兵表创建失败")
			}
			if _, err := database.ExecContext(ctx, "INSERT INTO sentinel(id) VALUES (731)"); err != nil {
				t.Fatal("隔离哨兵数据写入失败")
			}
			names := []string{"ordinary", "with space", "mail`prod", `mail"prod`,
				"quote`; DROP TABLE sentinel; --", `quote"; DROP TABLE sentinel; --`, "中文表名"}
			for _, name := range names {
				quoted := quoteDatabaseIdentifier(kind, name)
				if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoted+" (id INTEGER PRIMARY KEY)"); err != nil {
					t.Fatal("真实数据库未能按单个标识符创建测试表")
				}
				if _, err := database.ExecContext(ctx, "INSERT INTO "+quoted+" (id) VALUES (17)"); err != nil {
					t.Fatal("真实数据库未能按单个标识符写入测试表")
				}
				var actual int
				if database.QueryRowContext(ctx, "SELECT id FROM "+quoted).Scan(&actual) != nil || actual != 17 {
					t.Fatal("真实数据库标识符引用未保留数据")
				}
				if database.QueryRowContext(ctx, "SELECT id FROM sentinel").Scan(&actual) != nil || actual != 731 {
					t.Fatal("恶意标识符影响了隔离哨兵表")
				}
			}
		})
	}
}
