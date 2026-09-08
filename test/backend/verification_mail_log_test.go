package verification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/services/logstore"
	_ "modernc.org/sqlite"
)

func TestRelayMailLogRecordsResultWithoutChallengeSecrets(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "relay-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	migrations := migration.DefaultMigrations()
	for _, statement := range migrations[len(migrations)-1].StatementsFor(migration.DialectSQLite) {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := logstore.New(database, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	settings := SMTPSettings{Host: "localhost", Port: 25, Security: SecurityPlain, From: "sender@example.com", Username: "private-user", Password: "private-password"}
	for _, item := range []struct {
		cause  error
		result string
		count  int
	}{{nil, "accepted", 1}, {errors.New("private-remote-error"), "failure", 0}} {
		delivery, err := NewSMTPDelivery(settings)
		if err != nil {
			t.Fatal(err)
		}
		var message []byte
		delivery.send = func(_ SMTPSettings, _ string, data []byte) error {
			message = data
			return item.cause
		}
		err = delivery.Delivery()(Challenge{Email: "private-recipient@example.com", ID: "private-challenge", Purpose: PurposeLogin}, "private-code")
		if !errors.Is(err, item.cause) {
			t.Fatal("发送结果被改变")
		}
		for _, value := range message {
			if value != 0 {
				t.Fatal("验证码邮件缓冲未清理")
			}
		}
		entries, err := store.Query(context.Background(), logstore.Query{Kind: "mail", Limit: 1})
		if err != nil || len(entries) != 1 {
			t.Fatal("缺少中继发送记录", err)
		}
		entry := entries[0]
		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if entry.Result != item.result || payload["acceptedCount"] != float64(item.count) || payload["stage"] != "SMTP 中继投递" || payload["trigger"] != "系统安全验证邮件" || payload["outcome"] == "" {
			t.Fatalf("中继日志结果不完整：%+v", entry)
		}
		if strings.Contains(string(entry.Payload), "private-") {
			t.Fatal("中继日志包含敏感信息")
		}
	}
}
