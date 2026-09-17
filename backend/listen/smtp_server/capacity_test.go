package smtp_server

import (
	"errors"
	"net"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-smtp"
)

func TestSMTPBusyReturnsTemporaryAuthenticationFailure(t *testing.T) {
	engine, user, code := newSMTPProtocolAuthFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance, config.Instance = engine, &config.Config{Domain: "example.com"}
	t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
	session := &Session{RemoteAddress: &net.TCPAddr{IP: net.ParseIP("192.0.2.81"), Port: 465}, Ctx: &context.Context{}}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		var temporary *smtp.SMTPError
		err := session.AuthPlain("alice@example.com", code)
		if !errors.As(err, &temporary) || temporary.Code != 454 || temporary.EnhancedCode != (smtp.EnhancedCode{4, 7, 0}) || session.User != "" {
			t.Fatalf("SMTP 繁忙响应或会话状态错误：%v", err)
		}
	}
	release()
	if err := session.AuthPlain("alice@example.com", code); err != nil || session.User != user.Account {
		t.Fatalf("SMTP 繁忙恢复后无法认证：%v", err)
	}
}
