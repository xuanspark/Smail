package imap_server

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

func TestIMAPSessionGuardsAllDataCommands(t *testing.T) {
	var user models.User
	if has, err := db.Instance.Where("account = ?", "testCase").Get(&user); err != nil || !has {
		t.Fatalf("读取测试用户失败：%v", err)
	}
	service, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	code, item, err := service.CreateClientCredential(user.ID, "IMAP 撤销验收")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer().NewSession().(*authorizedSession)
	if err := s.Login("testCase@test.domain", code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Select("INBOX", &imap.SelectOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeClientCredential(user.ID, item.ID); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"select":      func() error { _, err := s.Select("INBOX", nil); return err },
		"create":      func() error { return s.Create("blocked", nil) },
		"delete":      func() error { return s.Delete("INBOX") },
		"rename":      func() error { return s.Rename("INBOX", "blocked") },
		"subscribe":   func() error { return s.Subscribe("INBOX") },
		"unsubscribe": func() error { return s.Unsubscribe("INBOX") },
		"list":        func() error { return s.List(nil, "", nil, nil) },
		"status":      func() error { _, err := s.Status("INBOX", nil); return err },
		"append":      func() error { _, err := s.Append("INBOX", nil, nil); return err },
		"expunge":     func() error { return s.Expunge(nil, nil) },
		"search":      func() error { _, err := s.Search(imapserver.NumKindUID, nil, nil); return err },
		"fetch":       func() error { return s.Fetch(nil, nil, nil) },
		"store":       func() error { return s.Store(nil, nil, nil, nil) },
		"copy":        func() error { _, err := s.Copy(nil, "INBOX"); return err },
		"move":        func() error { return s.Move(nil, nil, "INBOX") },
		"namespace":   func() error { _, err := s.Namespace(); return err },
		"idle":        func() error { return s.Idle(nil, nil) },
		"unselect":    s.Unselect,
		"poll":        func() error { return s.Poll(nil, true) },
		"idle_poll":   func() error { return s.serverSession.poll(nil, true, true) },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			var denied *imap.Error
			if err := check(); !errors.As(err, &denied) || denied.Code != imap.ResponseCodeAuthenticationFailed {
				t.Fatalf("撤销后仍执行数据命令：%v", err)
			}
		})
	}
}
