package recovery

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestPublicFlowEmailResetAndRoleBoundary(t *testing.T) {
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			s, verify, engine, user := newRecoveryFixture(t)
			migrations := migration.DefaultMigrations()[16:18]

			for index := range migrations {
				migrations[index].Version = int64(index + 1)
			}
			runner := migration.Runner{DB: engine.DB().DB, Dialect: migration.DialectSQLite, Migrations: migrations}
			if _, err := runner.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			logs, err := logstore.New(engine.DB().DB, migration.DialectSQLite)
			if err != nil {
				t.Fatal(err)
			}
			assertAudit := func(want int, secrets ...string) {
				t.Helper()
				entries, err := logs.Query(context.Background(), logstore.Query{Kind: "operation"})
				if err != nil || len(entries) != want {
					t.Fatalf("恢复审计记录数量不符：数量=%d，预期=%d，错误=%v", len(entries), want, err)
				}
				for _, entry := range entries {
					if entry.ActorID != strconv.Itoa(user.ID) || entry.ActorRole != string(role) || entry.Action != "公共密码恢复" || entry.Result != "success" {
						t.Fatal("恢复审计的可信身份或结果不符")
					}
					var payload map[string]string
					if err := json.Unmarshal(entry.Payload, &payload); err != nil {
						t.Fatal(err)
					}
					if payload["method"] != "email" || payload["actorName"] != user.Account || len(payload) != 5 {
						t.Fatal("恢复审计未使用预期的白名单字段")
					}
					for _, secret := range secrets {
						if secret != "" && strings.Contains(string(entry.Payload), secret) {
							t.Fatal("恢复审计包含敏感信息")
						}
					}
				}
			}
			user.Role = string(role)
			if _, err := engine.ID(user.ID).Cols("role").Update(user); err != nil {
				t.Fatal(err)
			}
			runtime := logstore.Start(logs)
			defer runtime.Stop()
			var code string
			verify.Send = func(_ verification.Challenge, value string) error { code = value; return nil }
			flow, err := s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			if role == access.RoleSuperAdmin {
				if flow.Token != "" || len(flow.Methods) != 0 || flow.Email != "" {
					t.Fatal("super administrator received public recovery access")
				}
				assertAudit(0)
				return
			}
			if flow.Email != "a***@example.com" || flow.Role != role {
				t.Fatalf("unexpected identity: %#v", flow)
			}
			if err := s.CompletePublicFlow(flow.Token, "NewPass123"); err == nil {
				t.Fatal("reset before verification")
			}
			if err := s.CompletePublicLink(flow.Token, "NewPass123", ""); err == nil {
				t.Fatal("flow accepted by legacy completion")
			}
			if err := s.SendPublicFlowCode(flow.Token, "192.0.2.1"); err != nil {
				t.Fatal(err)
			}
			if len(code) != 8 {
				t.Fatal("email code not delivered")
			}
			if _, err := s.VerifyPublicFlow(flow.Token, "email", "bad-code", "192.0.2.1"); err == nil {
				t.Fatal("invalid code accepted")
			}
			ticket, err := s.VerifyPublicFlow(flow.Token, "email", code, "192.0.2.1")
			if err != nil {
				t.Fatal(err)
			}
			if ticket == flow.Token {
				t.Fatal("verification token not rotated")
			}
			if _, err := s.VerifyPublicFlow(flow.Token, "email", code, "192.0.2.1"); err == nil {
				t.Fatal("verification replay accepted")
			}
			if err := s.CompletePublicFlow(ticket, "short"); err == nil {
				t.Fatal("invalid password accepted")
			}
			assertAudit(0)
			if err := s.CompletePublicFlow(ticket, "NewPass123"); err != nil {
				t.Fatal(err)
			}
			if err := s.CompletePublicFlow(ticket, "OtherPass12"); err == nil {
				t.Fatal("ticket replay accepted")
			}
			assertAudit(1, flow.Token, ticket, code, "NewPass123", "OtherPass12", "alice@example.com")
			var updated models.User
			if _, err := engine.ID(user.ID).Get(&updated); err != nil {
				t.Fatal(err)
			}
			if ok, err := password.Check(updated.Password, "NewPass123"); err != nil || !ok {
				t.Fatal("new password not applied", err)
			}
			if updated.SessionVersion != user.SessionVersion+1 {
				t.Fatal("old sessions still valid")
			}
		})
	}
}

func TestPublicFlowSecurityCodeRoleAndFailureLimits(t *testing.T) {
	for _, role := range []access.Role{access.RoleSuperAdmin, access.RoleAdmin, access.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			s, _, engine, user := newRecoveryFixture(t)
			user.Role = string(role)
			if _, err := engine.ID(user.ID).Cols("role").Update(user); err != nil {
				t.Fatal(err)
			}
			binding, err := s.external.FindVerifiedPrimaryByUser(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.external.Disable(user.ID, binding.ID); err != nil {
				t.Fatal(err)
			}
			codes, err := s.credentials.GenerateRecoveryCodes(user.ID, 5)
			if err != nil {
				t.Fatal(err)
			}
			flow, err := s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			if role == access.RoleSuperAdmin {
				if flow.Token != "" || len(flow.Methods) != 0 || flow.Email != "" {
					t.Fatal("超级管理员不应获得公共安全码恢复入口")
				}
				if _, err := s.VerifyPublicFlow(flow.Token, "security_code", codes[0], "192.0.2.2"); err == nil {
					t.Fatal("超级管理员安全码被公共恢复流程接受")
				}
				return
			}
			if flow.Token == "" || flow.Email != "" || len(flow.Methods) != 1 || flow.Methods[0] != "security_code" {
				t.Fatal("仅配置安全码的账号未获得独立恢复方式")
			}
			if err := s.CompletePublicFlow(flow.Token, "NewPass123"); err == nil {
				t.Fatal("未经安全码验证就完成了密码重置")
			}
			ticket, err := s.VerifyPublicFlow(flow.Token, "security_code", codes[0], "192.0.2.2")
			if err != nil {
				t.Fatal(err)
			}
			replay, err := s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.VerifyPublicFlow(replay.Token, "security_code", codes[0], "192.0.2.2"); err == nil {
				t.Fatal("已核销的安全码被重复使用")
			}
			if err := s.CompletePublicFlow(ticket, "NewPass123"); err != nil {
				t.Fatal(err)
			}
			var updated models.User
			if found, err := engine.ID(user.ID).Get(&updated); err != nil || !found {
				t.Fatal("无法读取重置后的账号", err)
			}
			if ok, err := password.Check(updated.Password, "NewPass123"); err != nil || !ok {
				t.Fatal("安全码恢复未更新密码", err)
			}
			if updated.SessionVersion != user.SessionVersion+1 || updated.SecurityVersion != user.SecurityVersion+1 {
				t.Fatal("安全码恢复未更新密码或撤销旧安全状态")
			}
			if configured, err := s.credentials.HasValidRecoveryCode(user.ID); err != nil || configured {
				t.Fatal("密码重置后仍保留旧安全码", err)
			}
			if err := s.CompletePublicFlow(ticket, "OtherPass123"); err == nil {
				t.Fatal("密码重置令牌被重复使用")
			}
			codes, err = s.credentials.GenerateRecoveryCodes(user.ID, 5)
			if err != nil {
				t.Fatal(err)
			}
			flow, err = s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < maxPublicRecoveryCodeAttempts; i++ {
				if _, err := s.VerifyPublicFlow(flow.Token, "security_code", "invalid", "192.0.2.2"); err == nil {
					t.Fatal("bad code accepted")
				}
			}
			if err := s.SendPublicFlowCode(flow.Token, "192.0.2.2"); err == nil {
				t.Fatal("exhausted flow usable")
			}
			if _, err := s.VerifyPublicFlow(flow.Token, "security_code", codes[0], "192.0.2.2"); err == nil {
				t.Fatal("达到失败次数上限后仍可验证有效安全码")
			}
		})
	}
}

func TestPublicFlowRejectsExpiredAndChangedAccount(t *testing.T) {
	for _, scenario := range []string{"expired", "disabled", "promoted", "security_version", "email_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			s, verify, engine, user := newRecoveryFixture(t)
			verify.Send = func(verification.Challenge, string) error { t.Fatal("sent code after invalidation"); return nil }
			flow, err := s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "expired":
				now := s.now()
				s.now = func() time.Time { return now.Add(20 * time.Minute) }
			case "disabled":
				user.Disabled = 1
				_, err = engine.ID(user.ID).Cols("disabled").Update(user)
			case "promoted":
				user.Role = string(access.RoleSuperAdmin)
				_, err = engine.ID(user.ID).Cols("role").Update(user)
			case "security_version":
				user.SecurityVersion++
				_, err = engine.ID(user.ID).Cols("security_version").Update(user)
			case "email_disabled":
				binding, lookup := s.external.FindVerifiedPrimaryByUser(user.ID)
				if lookup != nil {
					t.Fatal(lookup)
				}
				err = s.external.Disable(user.ID, binding.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SendPublicFlowCode(flow.Token, "192.0.2.3"); err == nil {
				t.Fatal("invalidated flow accepted")
			}
		})
	}
}

func TestPublicFlowRejectsCrossFlowCodeAndConcurrentUse(t *testing.T) {
	s, verify, _, user := newRecoveryFixture(t)
	first, err := s.IdentifyPublicFlow(user.Account)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.IdentifyPublicFlow(user.Account)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var code string
	verify.Send = func(_ verification.Challenge, value string) error {
		code = value
		close(started)
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- s.SendPublicFlowCode(first.Token, "192.0.2.4") }()
	<-started
	_, concurrentErr := s.VerifyPublicFlow(first.Token, "email", code, "192.0.2.4")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if concurrentErr == nil {
		t.Fatal("verification accepted while delivery was in progress")
	}
	// 即使另一流程取得挑战编号，也不能跨流程使用邮件验证码。
	s.publicTickets[ticketKey(second.Token)].ChallengeID = s.publicTickets[ticketKey(first.Token)].ChallengeID
	if _, err := s.VerifyPublicFlow(second.Token, "email", code, "192.0.2.4"); err == nil {
		t.Fatal("cross-flow email code accepted")
	}
	if _, err := s.VerifyPublicFlow(first.Token, "email", code, "192.0.2.4"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicFlowRechecksAccountAfterVerification(t *testing.T) {
	for _, scenario := range []string{"expired", "disabled", "promoted", "security_version", "email_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			s, verify, engine, user := newRecoveryFixture(t)
			var code string
			verify.Send = func(_ verification.Challenge, value string) error { code = value; return nil }
			flow, err := s.IdentifyPublicFlow(user.Account)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SendPublicFlowCode(flow.Token, "192.0.2.5"); err != nil {
				t.Fatal(err)
			}
			token, err := s.VerifyPublicFlow(flow.Token, "email", code, "192.0.2.5")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "expired":
				now := s.now()
				s.now = func() time.Time { return now.Add(20 * time.Minute) }
			case "disabled":
				user.Disabled = 1
				_, err = engine.ID(user.ID).Cols("disabled").Update(user)
			case "promoted":
				user.Role = string(access.RoleSuperAdmin)
				_, err = engine.ID(user.ID).Cols("role").Update(user)
			case "security_version":
				user.SecurityVersion++
				_, err = engine.ID(user.ID).Cols("security_version").Update(user)
			case "email_disabled":
				binding, lookup := s.external.FindVerifiedPrimaryByUser(user.ID)
				if lookup != nil {
					t.Fatal(lookup)
				}
				err = s.external.Disable(user.ID, binding.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CompletePublicFlow(token, "NewPass123"); err == nil {
				t.Fatal("password changed after account or verification invalidation")
			}
		})
	}
}
