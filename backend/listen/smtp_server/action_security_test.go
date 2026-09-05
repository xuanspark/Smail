package smtp_server

import (
	"errors"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-smtp"
)

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
