package logstore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/utils/logpolicy"
	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	items := migration.DefaultMigrations()
	for _, statement := range items[len(items)-1].StatementsFor(migration.DialectSQLite) {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(database, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestLogStoreEmptyEntriesAreNotInserted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, kind := range []string{"config", "operation", "mail"} {
		for _, payload := range []string{`{}`, `null`, `[]`, `" "`, `{"message":"***"}`} {
			if err := s.Write(ctx, Entry{Kind: kind, CreatedAt: time.Now(), Action: "---", Payload: []byte(payload)}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		table, _ := logTable(kind)
		if err := s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE kind = ?", kind).Scan(&count); err != nil || count != 0 {
			t.Fatalf("空记录已落库：%s %d %v", kind, count, err)
		}
		for _, entry := range []Entry{
			{Action: "登录验证", Result: "success", Payload: []byte(`{}`)},
			{Action: "update", Result: "success", Payload: []byte(`{"changes":[]}`)},
			{Result: "failure", Payload: []byte(`{}`)},
			{Result: "unknown", Payload: []byte(`{}`)},
			{Payload: []byte(`{"errorCode":0}`)},
			{Payload: []byte(`{"message":"投递成功"}`)},
			{Payload: []byte(`{"unknown":null}`)},
		} {
			entry.Kind, entry.CreatedAt = kind, time.Now()
			if err := s.Write(ctx, entry); err != nil {
				t.Fatal(err)
			}
		}
		items, err := s.Query(ctx, Query{Kind: kind})
		if err != nil || len(items) != 7 {
			t.Fatalf("有效证据丢失：%s %d %v", kind, len(items), err)
		}
	}
	if err := s.Write(ctx, Entry{Kind: "mail", CreatedAt: time.Now(), Payload: []byte(`invalid`)}); err != ErrInvalid {
		t.Fatal("过滤不能掩盖非法载荷")
	}
}

func TestLogRetentionBoundariesAndBusinessSafety(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 9, 8, 9, 37, 0, 0, time.UTC)
	s.now = func() time.Time { return now.Add(-logpolicy.Retention) }
	cutoff := logpolicy.Cutoff(now)
	ctx := context.Background()
	if _, err := s.db.Exec("CREATE TABLE business_probe (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO business_probe VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"operation", "mail"} {
		for index := 0; index < 2005; index++ {
			at := cutoff.Add(-time.Nanosecond)
			if index >= 2002 {
				at = cutoff.Add(time.Duration(index - 2002))
			}
			if err := s.Write(ctx, Entry{ID: fmt.Sprintf("%s-%d", kind, index), Kind: kind, CreatedAt: at, Result: "success", Payload: []byte(`{"message":"test"}`)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.now = func() time.Time { return now }
	for _, kind := range []string{"operation", "mail"} {
		items, err := s.Query(ctx, Query{Kind: kind})
		if err != nil || len(items) != 3 {
			t.Fatalf("visible %s = %d, %v", kind, len(items), err)
		}
	}
	deleted, err := s.Cleanup(ctx, now)
	if err != nil || deleted != 4004 {
		t.Fatalf("cleanup = %d, %v", deleted, err)
	}
	deleted, err = s.Cleanup(ctx, now)
	if err != nil || deleted != 0 {
		t.Fatalf("repeated cleanup = %d, %v", deleted, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM business_probe").Scan(&count); err != nil || count != 1 {
		t.Fatal("business data changed")
	}
	if err := s.Write(ctx, Entry{Kind: "mail", CreatedAt: cutoff.Add(-1), Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM mail_log").Scan(&count); err != nil || count != 3 {
		t.Fatal("expired replay was inserted")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Cleanup(ctx, now); err == nil {
		t.Fatal("cancelled cleanup succeeded")
	}
}

func TestLogDialectStatementsAndAuditReplay(t *testing.T) {
	s := testStore(t)
	for _, dialect := range []migration.Dialect{migration.DialectSQLite, migration.DialectMySQL, migration.DialectPostgres} {
		s.dialect = dialect
		got := s.bind("SELECT id FROM mail_log WHERE created_at < ? LIMIT ?")
		if dialect == migration.DialectPostgres {
			if !strings.Contains(got, "$1 LIMIT $2") {
				t.Fatal(got)
			}
		} else if strings.Count(got, "?") != 2 {
			t.Fatal(got)
		}
	}
	s.dialect = migration.DialectSQLite
	audit := &AuditWriter{Store: s}
	event, err := config.NormalizeConfigAuditEvent(config.ConfigAuditEvent{
		Operation: config.ConfigOperationUpdate, Section: config.SectionRoot, ActorID: "1", ActorRole: "super_admin", ActorName: "tester",
		FromGeneration: 1, ToGeneration: 2, CreatedAt: time.Now(), Result: "success",
		Changes: []config.ConfigAuditChange{{Field: "root.logLevel", Before: "info", After: "debug"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := audit.WriteConfigAudit(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := audit.Query(context.Background(), config.ConfigAuditQuery{})
	if err != nil || len(items) != 1 || items[0].Changes[0].After != "debug" {
		t.Fatalf("audit: %+v, %v", items, err)
	}
	data, err := audit.ExportCSV(context.Background(), config.ConfigAuditQuery{})
	if err != nil || !strings.Contains(string(data), "tester") || !strings.Contains(string(data), "debug") {
		t.Fatalf("csv: %s, %v", data, err)
	}
}
