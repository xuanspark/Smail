package smtp_server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestInboundMailLogBelongsToEachActualRecipient(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	config.Instance.SpamFilterLevel = 0
	users := []models.User{{Account: "alice", Name: "Alice", Role: "user"}, {Account: "bob", Name: "Bob", Role: "user"}}
	for i := range users {
		if _, err := engine.Insert(&users[i]); err != nil {
			t.Fatal(err)
		}
	}
	migrations := migration.DefaultMigrations()
	for _, statement := range migrations[len(migrations)-1].StatementsFor(migration.DialectSQLite) {
		if _, err := engine.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := logstore.New(engine.DB().DB, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	session := &Session{Ctx: &appcontext.Context{Context: context.Background()}, From: "sender@example.net", To: []string{"alice@example.com", "bob@example.com", "alice@example.com"}, RemoteAddress: &net.TCPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 25}}
	message := "From: sender@example.net\r\nTo: alice@example.com\r\nSubject: private-subject\r\nMessage-ID: <scope@example.net>\r\n\r\nprivate-body"
	if err := session.Data(strings.NewReader(message)); err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		entries, err := store.Query(context.Background(), logstore.Query{Kind: "mail", ScopeActorID: strconv.Itoa(user.ID)})
		if err != nil || len(entries) != 1 {
			t.Fatalf("收件人日志重复或缺失：%d %d %v", user.ID, len(entries), err)
		}
		var payload struct {
			RecipientCount int `json:"recipientCount"`
		}
		if err := json.Unmarshal(entries[0].Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.RecipientCount != 1 || entries[0].Result != "success" || strings.Contains(string(entries[0].Payload), "private-") || strings.Contains(string(entries[0].Payload), "bob@") {
			t.Fatalf("收信范围或结果错误：%+v", entries[0])
		}
	}
}

type failedMailReader struct{}

func (failedMailReader) Read([]byte) (int, error) { return 0, errors.New("private-reader-error") }

func TestSMTPDataLogDistinguishesDiscardedMailAndReadFailure(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	config.Instance.SpamFilterLevel = 3
	migrations := migration.DefaultMigrations()
	for _, statement := range migrations[len(migrations)-1].StatementsFor(migration.DialectSQLite) {
		if _, err := engine.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := logstore.New(engine.DB().DB, migration.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := logstore.Start(store)
	defer runtime.Stop()
	session := &Session{
		Ctx:  &appcontext.Context{Context: context.Background()},
		From: "sender@example.net", To: []string{"absent@example.com"},
		RemoteAddress: &net.TCPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 25},
	}
	message := "From: sender@example.net\r\nTo: absent@example.com\r\nSubject: private-subject\r\n\r\nprivate-body"
	if err := session.Data(bytes.NewBufferString(message)); err != nil {
		t.Fatal(err)
	}
	assertAuditCounts(t, engine, 0, 0)
	for _, result := range []string{"discarded", "failure"} {
		if result == "failure" && session.Data(failedMailReader{}) == nil {
			t.Fatal("读取失败未返回错误")
		}
		entries, err := store.Query(context.Background(), logstore.Query{Kind: "mail", Limit: 1})
		if err != nil || len(entries) != 1 {
			t.Fatal("缺少收信处理记录", err)
		}
		entry := entries[0]
		var details logstore.MailDetails
		if err := json.Unmarshal(entry.Payload, &details); err != nil {
			t.Fatal(err)
		}
		if entry.Result != result || details.Trigger != "SMTP 收信" || details.Stage == "" || details.Outcome == "" {
			t.Fatalf("收信状态错误：%+v", entry)
		}
		if strings.Contains(string(entry.Payload), "private-") {
			t.Fatal("收信日志泄露内容或原始错误")
		}
	}
}
