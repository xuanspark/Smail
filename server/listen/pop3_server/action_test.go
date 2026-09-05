package pop3_server

import (
	"bytes"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-message/mail"
	"io"
	"path/filepath"
	"testing"
)

func Test_action_Retr(t *testing.T) {
	testRoot := t.TempDir()
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "pop3.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})

	a := action{}
	session := &gopop.Session{
		Ctx: &context.Context{
			UserID: 1,
		},
	}
	got, got1, err := a.Retr(session, 301)

	_, _, _ = got, got1, err
}

func Test_email(t *testing.T) {
	var b bytes.Buffer

	// Create our mail header
	var h mail.Header

	// Create a new mail writer
	mw, _ := mail.CreateWriter(&b, h)

	// Create a text part
	tw, _ := mw.CreateInline()

	var html mail.InlineHeader

	html.Header.Set("Content-Transfer-Encoding", "base64")
	w, _ := tw.CreatePart(html)

	io.WriteString(w, "=")

	w.Close()

	tw.Close()

	fmt.Printf("%s", b.String())

}

func TestActionUserPreservesFullEmailIdentifier(t *testing.T) {
	session := &gopop.Session{}
	if err := (action{}).User(session, "Alice@Example.com"); err != nil {
		t.Fatal(err)
	}
	if session.User != "Alice@Example.com" {
		t.Fatalf("POP3 user identifier = %q, want full email identifier", session.User)
	}
}

func TestActionUidlOwnershipValidation(t *testing.T) {
	testRoot := t.TempDir()
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(testRoot, "pop3_uidl.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})

	a := action{}
	session := &gopop.Session{
		Ctx: &context.Context{
			UserID: 1,
		},
	}
	// 查询未授权/不存在的邮件 ID 应返回错误
	_, err := a.Uidl(session, "99999")
	if err == nil {
		t.Fatal("expected error for unauthorized/non-existent message UIDL query, got nil")
	}
}
