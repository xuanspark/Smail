package credentials

import (
	"strings"
	"testing"
)

func TestRevokeAllRejectsClientCodesAndPreservesOtherUsers(t *testing.T) {
	for _, mode := range []string{"standalone", "transaction", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			service := newFixture(t)
			service.SetVaultRoot(t.TempDir())
			codes := make(map[int]string)
			for _, userID := range []int{1, 2} {
				code, _, err := service.CreateClientCredential(userID, "测试客户端")
				if err != nil {
					t.Fatal(err)
				}
				codes[userID] = code
				if matched, err := service.CheckClientOrProtocolCode(userID, ProtocolSMTP, code); err != nil || !matched {
					t.Fatalf("撤销前客户端码无法登录：%v", err)
				}
			}
			if mode == "standalone" {
				if err := service.RevokeAll(1); err != nil {
					t.Fatal(err)
				}
			} else {
				tx := service.engine.NewSession()
				defer tx.Close()
				if err := tx.Begin(); err != nil {
					t.Fatal(err)
				}
				if err := service.RevokeAllWithSession(tx, 1); err != nil {
					t.Fatal(err)
				}
				var err error
				if mode == "rollback" {
					err = tx.Rollback()
				} else {
					err = tx.Commit()
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for userID, code := range codes {
				want := userID == 2 || mode == "rollback"
				for _, protocol := range SupportedProtocols() {
					matched, err := service.CheckClientOrProtocolCode(userID, protocol, code)
					if err != nil || matched != want {
						t.Fatalf("用户 %d 协议 %s 的撤销状态错误：%v", userID, protocol, err)
					}
				}
				revealed, err := service.RevealClientCredentials(userID)
				if err != nil || (len(revealed) > 0) != want {
					t.Fatalf("用户 %d 的授权码展示状态错误：%v", userID, err)
				}
			}
		})
	}
}

func TestRevokeAllRollsBackWhenClientRevocationFails(t *testing.T) {
	service := newFixture(t)
	if err := service.SetSecurityQuestion(1, "测试问题", "测试答案"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateClientCredential(1, "测试客户端"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.engine.Exec("CREATE TRIGGER reject_client_revocation BEFORE UPDATE ON client_credential BEGIN SELECT RAISE(ABORT, 'injected revocation failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAll(1); err == nil || !strings.Contains(err.Error(), "injected revocation failure") {
		t.Fatalf("未命中客户端撤销故障：%v", err)
	}
	if matched, err := service.VerifySecurityQuestion(1, "测试答案"); err != nil || !matched {
		t.Fatalf("前序撤销未回滚：%v", err)
	}
}
