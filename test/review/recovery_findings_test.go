package recovery

import (
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/utils/password"
)

// 审查专用：从已验证后签发票据的位置开始，仅验证撤销后的授权有效性。
// 本测试不模拟或绕过真实 WebAuthn 签名验证，也不调用真实服务。
func TestAuditRevokedWebAuthnMustInvalidateRecoveryTicket(t *testing.T) {
	service, _, engine, user := newRecoveryFixture(t)
	credential := models.WebAuthnCredential{
		UserID: user.ID, CredentialID: "audit-credential",
		CredentialData: "unused", DisplayName: "audit", Discoverable: true, CreatedAt: time.Now(),
	}
	if _, err := engine.Insert(&credential); err != nil {
		t.Fatal(err)
	}
	verifier, err := webauthnservice.NewService(engine, webauthnservice.Config{
		RPID: "localhost", RPDisplayName: "audit", RPOrigins: []string{"https://localhost"}, VaultRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := service.issueTicketFor(user.ID, user.SessionVersion, "webauthn", credential.ID, 0, "audit-browser")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.RevokeCredential(user.ID, credential.ID); err != nil {
		t.Fatal(err)
	}
	var revoked models.WebAuthnCredential
	if found, err := engine.ID(credential.ID).Get(&revoked); err != nil || !found || revoked.RevokedAt == nil {
		t.Fatalf("撤销凭据的前置条件未成立: found=%v err=%v", found, err)
	}
	const newPassword = "AuditPass123"
	if err := password.ValidateUserPassword(newPassword); err != nil {
		t.Fatalf("测试密码不符合当前策略: %v", err)
	}
	err = service.CompleteWithContext(ticketID, newPassword, "audit-browser")
	var updated models.User
	if found, lookupErr := engine.ID(user.ID).Get(&updated); lookupErr != nil || !found {
		t.Fatal("无法检查临时账户")
	}
	t.Logf("revoked=true completion_error=%v password_changed=%v", err, updated.Password != user.Password)
	if errors.Is(err, ErrRecoveryDenied) || errors.Is(err, ErrRecoveryExpired) {
		if updated.Password != user.Password {
			t.Fatal("恢复被拒绝后不应改变密码")
		}
		return
	}
	if err != nil {
		t.Fatalf("出现非授权类错误，本次结果不能证明撤销失效: %v", err)
	}
	if !password.Verify(updated.Password, newPassword) {
		t.Fatal("完成接口未返回错误，但新密码未生效，需要单独诊断")
	}
	t.Fatal("用于签发恢复票据的安全密钥已经撤销，但旧票据仍然允许修改密码")
}
