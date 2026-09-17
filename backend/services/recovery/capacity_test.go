package recovery

import (
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestRecoveryBusyPreservesTicket(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, password.ErrBusy) {
			t.Fatalf("繁忙期间票据失效或返回错误：%v", err)
		}
	}
	release()
	if err := service.Complete(ticketID, "NewPass123"); err != nil {
		t.Fatalf("繁忙恢复后不能使用原票据：%v", err)
	}
	if err := service.Complete(ticketID, "OtherPass12"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("成功后票据未保持一次性：%v", err)
	}
}

func TestRecoveryBusyDoesNotRestoreRevokedTicket(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	service.InvalidateUser(user.ID)
	release()
	if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("撤销的票据被恢复：%v", err)
	}
}

func TestEmailContinuationBusyPreservesAttemptsAndCode(t *testing.T) {
	service, verify, engine, user := newRecoveryFixture(t)
	user.ApplyRole(access.RoleAdmin)
	if _, err := engine.ID(user.ID).Cols("role", "is_admin").Update(user); err != nil {
		t.Fatal(err)
	}
	codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	var code string
	verify.Send = func(_ verification.Challenge, issued string) error { code = issued; return nil }
	challenge, err := service.Begin(BeginRequest{Identifier: user.Account, Email: "alice@example.com", BrowserContext: "capacity-browser"}, "192.0.2.80")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.VerifyEmailStepWithContext(challenge, user.Account, "alice@example.com", code, "192.0.2.80", "capacity-browser")
	if err != nil || !result.RequiresRecoveryCode || result.Continuation == "" {
		t.Fatalf("管理员邮箱验证未生成中间凭证：%v", err)
	}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		if _, err := service.ContinueEmailWithContext(result.Continuation, user.Account, "alice@example.com", codes[0], "capacity-browser"); !errors.Is(err, password.ErrBusy) {
			t.Fatalf("繁忙时中间凭证不可重试：%v", err)
		}
	}
	pending := service.emailPending[ticketKey(result.Continuation)]
	if pending == nil || pending.Busy || pending.Attempts != 0 {
		t.Fatal("繁忙误计失败次数或未释放中间凭证")
	}
	release()
	ticketID, err := service.ContinueEmailWithContext(result.Continuation, user.Account, "alice@example.com", codes[0], "capacity-browser")
	if err != nil || ticketID == "" {
		t.Fatalf("繁忙结束后无法继续恢复：%v", err)
	}
	if _, err := service.ContinueEmailWithContext(result.Continuation, user.Account, "alice@example.com", codes[0], "capacity-browser"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("中间凭证被重复使用：%v", err)
	}
	if err := service.CompleteWithContext(ticketID, "NewPass123", "capacity-browser"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicLinkBusyPreservesAttemptsAndSingleUse(t *testing.T) {
	for _, role := range []access.Role{access.RoleUser, access.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			service, _, engine, user := newRecoveryFixture(t)
			user.ApplyRole(role)
			if _, err := engine.ID(user.ID).Cols("role", "is_admin").Update(user); err != nil {
				t.Fatal(err)
			}
			var recoveryCode string
			if role == access.RoleAdmin {
				codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
				if err != nil {
					t.Fatal(err)
				}
				recoveryCode = codes[0]
			}
			var token string
			service.SetPublicLinkDelivery(func(_ string, issued string) error { token = issued; return nil })
			if err := service.BeginPublicLink(user.Account); err != nil || token == "" {
				t.Fatalf("未生成恢复链接：%v", err)
			}
			release := passwordtest.Hold(t)
			for index := 0; index < 12; index++ {
				if err := service.CompletePublicLink(token, "NewPass123", recoveryCode); !errors.Is(err, password.ErrBusy) {
					t.Fatalf("繁忙时链接不可重试：%v", err)
				}
			}
			pending := service.publicTickets[ticketKey(token)]
			if pending == nil || pending.Busy || pending.Attempts != 0 {
				t.Fatal("繁忙误计失败次数或未释放链接")
			}
			release()
			if err := service.CompletePublicLink(token, "NewPass123", recoveryCode); err != nil {
				t.Fatal(err)
			}
			if err := service.CompletePublicLink(token, "NextPass12", recoveryCode); !errors.Is(err, ErrRecoveryExpired) {
				t.Fatalf("恢复链接被重复使用：%v", err)
			}
			var stored models.User
			if found, err := engine.ID(user.ID).Get(&stored); err != nil || !found || stored.SessionVersion != user.SessionVersion+1 {
				t.Fatalf("重置未撤销旧会话版本：%v", err)
			}
			if matched, err := password.Check(stored.Password, "NewPass123"); !matched || err != nil {
				t.Fatalf("新密码不可用：%v", err)
			}
		})
	}
}

func TestPublicLinkBusyDuringCodeCheckRollsBack(t *testing.T) {
	service, _, engine, user := newRecoveryFixture(t)
	user.ApplyRole(access.RoleAdmin)
	if _, err := engine.ID(user.ID).Cols("role", "is_admin").Update(user); err != nil {
		t.Fatal(err)
	}
	codes, err := service.credentials.GenerateRecoveryCodes(user.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	service.SetPublicLinkDelivery(func(_ string, issued string) error { token = issued; return nil })
	if err := service.BeginPublicLink(user.Account); err != nil {
		t.Fatal(err)
	}
	now, calls := service.now, 0
	var release func()
	// 第二次读取时钟位于新密码哈希之后、恢复码事务之前。
	service.now = func() time.Time {
		calls++
		if calls == 2 {
			release = passwordtest.Hold(t)
		}
		return now()
	}
	if err := service.CompletePublicLink(token, "NewPass123", codes[0]); !errors.Is(err, password.ErrBusy) || release == nil {
		t.Fatalf("未覆盖事务内恢复码繁忙分支：%v", err)
	}
	service.now = now
	release()
	pending := service.publicTickets[ticketKey(token)]
	if pending == nil || pending.Busy || pending.Attempts != 0 {
		t.Fatal("恢复码繁忙错误地消耗了链接尝试次数")
	}
	var stored models.User
	if found, err := engine.ID(user.ID).Get(&stored); err != nil || !found || stored.Password != user.Password || stored.SessionVersion != user.SessionVersion {
		t.Fatalf("繁忙事务未保持原密码和会话版本：%v", err)
	}
	if err := service.CompletePublicLink(token, "NewPass123", codes[0]); err != nil {
		t.Fatalf("事务繁忙后同一链接及恢复码不可用：%v", err)
	}
}
