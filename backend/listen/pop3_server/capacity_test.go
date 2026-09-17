package pop3_server

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestPOP3BusyReturnsTemporaryAuthenticationFailure(t *testing.T) {
	engine, user, code := newPOP3ProtocolAuthFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance, config.Instance = engine, &config.Config{Domain: "example.com"}
	t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
	ctx := &context.Context{}
	session := &gopop.Session{User: "alice@example.com", InTls: true, Ctx: ctx}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		err := (action{}).Pass(session, code)
		if !errors.Is(err, gopop.ErrAuthenticationTemporary) || session.Status == gopop.TRANSACTION || ctx.UserID != 0 {
			t.Fatalf("POP3 繁忙响应或会话状态错误：%v", err)
		}
	}
	release()
	if err := (action{}).Pass(session, code); err != nil || ctx.UserID != user.ID {
		t.Fatalf("POP3 繁忙恢复后无法认证：%v", err)
	}
}
