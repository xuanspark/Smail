package adminreset

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/password"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestResetSuperAdminOnlyRevokesTargetSessionsAndCredentials(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	for _, table := range []any{&models.User{}, &models.Sessions{}, &models.SecurityQuestion{}, &models.RecoveryCode{}, &models.WebAuthnCredential{}, &models.ProtocolCredential{}, &models.ClientCredential{}} {
		if err := engine.Sync2(table); err != nil {
			t.Fatal(err)
		}
	}
	superAdmin := models.User{Account: "root", Name: "超级管理员", Role: "super_admin", IsAdmin: 1, SessionVersion: 3}
	if err := auth.SetPassword(&superAdmin, "OldPass123"); err != nil {
		t.Fatal(err)
	}
	ordinaryUser := models.User{Account: "alice", Name: "普通用户", Role: "user", SessionVersion: 7}
	if err := auth.SetPassword(&ordinaryUser, "UserPass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&superAdmin, &ordinaryUser); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(
		&models.Sessions{Token: "super-token", Data: "opaque-super", Expiry: 4102444800},
		&models.Sessions{Token: "user-token", Data: "opaque-user", Expiry: 4102444800},
		&models.SecurityQuestion{UserID: superAdmin.ID, Question: "问题", AnswerHash: "hash", Version: 1},
		&models.SecurityQuestion{UserID: ordinaryUser.ID, Question: "问题", AnswerHash: "hash", Version: 1},
		&models.RecoveryCode{UserID: superAdmin.ID, CodeHash: "hash", CodeVersion: 1},
		&models.RecoveryCode{UserID: ordinaryUser.ID, CodeHash: "hash", CodeVersion: 1},
		&models.ProtocolCredential{UserID: superAdmin.ID, Protocol: "smtp", Enabled: true, CodeHash: "hash", CodeVersion: 1},
		&models.ProtocolCredential{UserID: ordinaryUser.ID, Protocol: "smtp", Enabled: true, CodeHash: "hash", CodeVersion: 1},
		&models.WebAuthnCredential{UserID: superAdmin.ID, CredentialID: "super-key", CredentialData: "encrypted", DisplayName: "管理密钥", CreatedAt: time.Now()},
		&models.WebAuthnCredential{UserID: ordinaryUser.ID, CredentialID: "user-key", CredentialData: "encrypted", DisplayName: "用户密钥", CreatedAt: time.Now()},
	); err != nil {
		t.Fatal(err)
	}
	invalidatedUserID := 0
	invalidatedAccount := ""
	if err := ResetSuperAdmin(engine, "NewPass123", func(userID int, account string) {
		invalidatedUserID = userID
		invalidatedAccount = account
	}); err != nil {
		t.Fatal(err)
	}
	if invalidatedUserID != superAdmin.ID || invalidatedAccount != superAdmin.Account {
		t.Fatalf("runtime invalidation target = %d/%s", invalidatedUserID, invalidatedAccount)
	}
	var updated models.User
	found, err := engine.ID(superAdmin.ID).Get(&updated)
	if err != nil || !found {
		t.Fatalf("load updated user: %v", err)
	}
	if !password.Verify(updated.Password, "NewPass123") || updated.SessionVersion != 4 {
		t.Fatalf("password/session version not updated: %+v", updated)
	}
	count, err := engine.Count(&models.Sessions{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("other sessions were deleted: %d", count)
	}
	var unchanged models.User
	if found, err := engine.ID(ordinaryUser.ID).Get(&unchanged); err != nil || !found || unchanged.SessionVersion != 7 {
		t.Fatalf("ordinary user session version changed: %+v, %v", unchanged, err)
	}
	if count, err := engine.Where("user_id = ?", superAdmin.ID).Count(&models.SecurityQuestion{}); err != nil || count != 0 {
		t.Fatalf("super administrator security question count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND used_at IS NULL", superAdmin.ID).Count(&models.RecoveryCode{}); err != nil || count != 0 {
		t.Fatalf("super administrator active recovery code count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND revoked_at IS NULL", superAdmin.ID).Count(&models.WebAuthnCredential{}); err != nil || count != 0 {
		t.Fatalf("super administrator active WebAuthn count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND revoked_at IS NULL", superAdmin.ID).Count(&models.ProtocolCredential{}); err != nil || count != 0 {
		t.Fatalf("super administrator active protocol credential count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ?", ordinaryUser.ID).Count(&models.SecurityQuestion{}); err != nil || count != 1 {
		t.Fatalf("ordinary user security question count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND used_at IS NULL", ordinaryUser.ID).Count(&models.RecoveryCode{}); err != nil || count != 1 {
		t.Fatalf("ordinary user active recovery code count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND revoked_at IS NULL", ordinaryUser.ID).Count(&models.WebAuthnCredential{}); err != nil || count != 1 {
		t.Fatalf("ordinary user active WebAuthn count = %d, err = %v", count, err)
	}
	if count, err := engine.Where("user_id = ? AND revoked_at IS NULL", ordinaryUser.ID).Count(&models.ProtocolCredential{}); err != nil || count != 1 {
		t.Fatalf("ordinary user active protocol credential count = %d, err = %v", count, err)
	}
}

func TestResetSuperAdminRollsBackWhenCredentialRevocationFails(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.User{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "root", Name: "超级管理员", Role: "super_admin", IsAdmin: 1, SessionVersion: 9}
	if err := auth.SetPassword(&user, "OldPass123"); err != nil {
		t.Fatal(err)
	}
	oldHash := user.Password
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	if err := ResetSuperAdmin(engine, "NewPass123", func(int, string) { invalidated = true }); err == nil {
		t.Fatal("missing credential tables did not fail reset")
	}
	if invalidated {
		t.Fatal("runtime state was invalidated before transaction commit")
	}
	var unchanged models.User
	if found, err := engine.ID(user.ID).Get(&unchanged); err != nil || !found {
		t.Fatalf("load rolled back user: %v", err)
	}
	if unchanged.Password != oldHash || unchanged.SessionVersion != 9 {
		t.Fatalf("password transaction was not rolled back: %+v", unchanged)
	}
}

func TestResetSuperAdminRejectsMissingTarget(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.User{}, &models.Sessions{}, &models.SecurityQuestion{}, &models.RecoveryCode{}, &models.WebAuthnCredential{}); err != nil {
		t.Fatal(err)
	}
	if err := ResetSuperAdmin(engine, "NewPass123"); err != ErrSuperAdminNotFound {
		t.Fatalf("error = %v", err)
	}
}

func TestControlServerInterfaceCompiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ListenControl(filepath.Join(t.TempDir(), "control.sock"), []uint32{0}); err == nil {
		// 具体平台实现由各自适配器测试覆盖。
	}
	_ = ctx
}
