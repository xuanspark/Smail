package logstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/utils/logpolicy"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// 禁止 database/sql 在断线后重建连接，以免临时表消失后误访问同名的永久表。
type logTestConnector struct {
	driver.Connector
	mu   sync.Mutex
	used bool
}

func (c *logTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		return nil, errors.New("temporary log test connection cannot reconnect")
	}
	c.used = true
	return c.Connector.Connect(ctx)
}

// 使用连接私有的临时表，关闭连接自动回收，不迁移或清空任何既有业务表。
func TestLogStoreServerDatabaseIntegration(t *testing.T) {
	for _, test := range []struct {
		name, driver, environment string
		dialect                   migration.Dialect
	}{
		{"mysql", "mysql", "SMAIL_TEST_MYSQL_DSN", migration.DialectMySQL},
		{"postgres", "postgres", "SMAIL_TEST_POSTGRES_DSN", migration.DialectPostgres},
	} {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.environment))
			if dsn == "" {
				t.Skip(test.environment + " is not set")
			}
			var connector driver.Connector
			var err error
			if test.dialect == migration.DialectMySQL {
				configuration, parseErr := mysql.ParseDSN(dsn)
				if parseErr != nil {
					t.Fatal("test database connection configuration is invalid")
				}
				connector, err = mysql.NewConnector(configuration)
			} else {
				connector, err = pq.NewConnector(dsn)
			}
			if err != nil {
				t.Fatal("test database connection configuration is invalid")
			}
			database := sql.OpenDB(&logTestConnector{Connector: connector})
			defer database.Close()
			database.SetMaxOpenConns(1)
			database.SetMaxIdleConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := database.PingContext(ctx); err != nil {
				t.Fatal("dedicated test database is unavailable")
			}
			migrations := migration.DefaultMigrations()
			for _, statement := range migrations[len(migrations)-1].StatementsFor(test.dialect) {
				if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS ") {
					statement = strings.Replace(statement, "CREATE TABLE IF NOT EXISTS ", "CREATE TEMPORARY TABLE ", 1)
				}
				if _, err := database.ExecContext(ctx, statement); err != nil {
					t.Fatal("temporary log schema creation failed")
				}
			}
			store, err := New(database, test.dialect)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			cutoff := logpolicy.Cutoff(now)
			store.now = func() time.Time { return now.Add(-logpolicy.Retention) }
			for _, kind := range []string{"operation", "mail"} {
				for index := 0; index < 1003; index++ {
					at := cutoff.Add(-time.Nanosecond)
					if index >= 1001 {
						at = cutoff.Add(time.Duration(index - 1001))
					}
					entry := Entry{ID: fmt.Sprintf("%s-%d", kind, index), CreatedAt: at, Kind: kind, ActorID: "42", Action: "兼容验证", Result: "success", Payload: []byte(`{"message":"中文日志"}`)}
					if err := store.Write(ctx, entry); err != nil {
						t.Fatal(err)
					}
					if index == 1002 {
						if err := store.Write(ctx, entry); err != nil {
							t.Fatal("idempotent replay failed")
						}
					}
				}
			}
			store.now = func() time.Time { return now }
			for _, kind := range []string{"operation", "mail"} {
				items, err := store.Query(ctx, Query{Kind: kind, ActorID: "42"})
				if err != nil || len(items) != 2 || items[0].Action != "兼容验证" {
					t.Fatalf("query or retention failed: kind=%s count=%d err=%v", kind, len(items), err)
				}
			}
			deleted, err := store.Cleanup(ctx, now)
			if err != nil || deleted != 2002 {
				t.Fatalf("batch cleanup failed: deleted=%d err=%v", deleted, err)
			}
			if deleted, err := store.Cleanup(ctx, now); err != nil || deleted != 0 {
				t.Fatalf("repeated cleanup failed: deleted=%d err=%v", deleted, err)
			}
		})
	}
}
