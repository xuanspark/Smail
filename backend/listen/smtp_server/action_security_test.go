package smtp_server

import (
	"errors"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-smtp"
)

func TestAnonymousRcptRejectsForeignDomains(t *testing.T) {
	oldConfig := config.Instance
	t.Cleanup(func() { config.Instance = oldConfig })
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com", "alias.example"}}
	for _, tt := range []struct {
		address string
		allowed bool
	}{
		{"alice@example.com", true},
		{"alice@EXAMPLE.COM", true},
		{"alice@alias.example", true},
		{"alice@external.example", false},
		{"alice@example.com.attacker.test", false},
		{"alice@sub.example.com", false},
		{"not-an-address", false},
	} {
		t.Run(tt.address, func(t *testing.T) {
			session := &Session{Ctx: &context.Context{}, To: []string{"existing@example.com"}}
			err := session.Rcpt(tt.address, nil)
			if tt.allowed {
				if err != nil || len(session.To) != 2 {
					t.Fatalf("受管域名收件失败：%v", err)
				}
				return
			}
			var denied *smtp.SMTPError
			if !errors.As(err, &denied) || denied.Code != 550 || len(session.To) != 1 {
				t.Fatalf("匿名外域收件未明确拒绝或污染收件列表：%v", err)
			}
		})
	}
}

func TestSessionResetClearsTransactionStateOnly(t *testing.T) {
	ctx := &context.Context{UserID: 42, UserAccount: "smail", IsAdmin: true}
	session := &Session{
		User: "smail",
		From: "sender@example.com",
		To:   []string{"first@example.com", "second@example.com"},
		Ctx:  ctx,
	}

	session.Reset()

	if session.From != "" || len(session.To) != 0 {
		t.Fatalf("transaction state was not cleared: from=%q to=%v", session.From, session.To)
	}
	if session.User != "smail" || session.Ctx != ctx || session.Ctx.UserID != 42 {
		t.Fatal("authenticated session state was cleared")
	}
}

func TestDataRejectsMalformedMessage(t *testing.T) {
	session := &Session{Ctx: &context.Context{}, To: []string{"recipient@example.com"}}
	err := session.Data(strings.NewReader("invalid header\r\n\r\nbody"))
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) || smtpErr.Code != 550 {
		t.Fatalf("unexpected malformed message error: %v", err)
	}
}

func TestDataRejectsMissingSender(t *testing.T) {
	session := &Session{Ctx: &context.Context{}, To: []string{"recipient@example.com"}}
	message := "To: recipient@example.com\r\nSubject: missing sender\r\n\r\nbody"
	err := session.Data(strings.NewReader(message))
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) || smtpErr.Code != 550 {
		t.Fatalf("unexpected missing sender error: %v", err)
	}
}
