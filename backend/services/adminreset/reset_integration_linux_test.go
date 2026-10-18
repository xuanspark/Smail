//go:build linux

package adminreset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/password"
	"xorm.io/xorm"
)

const integrationOldPassword = "OldPass123"
const integrationNewPassword = "NewPass456"

func resetIntegrationDatabase(t *testing.T) (*xorm.Engine, []models.User) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Sync2(&models.User{}, &models.SecurityQuestion{}, &models.RecoveryCode{},
		&models.WebAuthnCredential{}, &models.ProtocolCredential{}, &models.ClientCredential{}); err != nil {
		t.Fatal(err)
	}
	hash, err := password.HashUserPassword(integrationOldPassword)
	if err != nil {
		t.Fatal(err)
	}
	clientHash, err := password.Hash(credentials.CleanAuthCode(integrationOldPassword))
	if err != nil {
		t.Fatal(err)
	}
	users := []models.User{
		{Account: "root", Name: "恢复管理员", Role: "super_admin", IsAdmin: 1,
			Password: hash, SessionVersion: 3, SecurityVersion: 5, MustChangePassword: 1},
		{Account: "alice", Name: "普通用户", Role: "user",
			Password: hash, SessionVersion: 7, SecurityVersion: 9},
	}
	for index := range users {
		user := &users[index]
		if _, err := engine.Insert(user); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Insert(
			&models.SecurityQuestion{UserID: user.ID, Question: "测试问题", AnswerHash: hash, Version: 1},
			&models.RecoveryCode{UserID: user.ID, CodeHash: hash, CodeVersion: 1},
			&models.ClientCredential{UserID: user.ID, ClientName: "测试客户端", CodeHash: clientHash, CodeVersion: 1},
			&models.WebAuthnCredential{UserID: user.ID, CredentialID: user.Account + "-key",
				CredentialData: "test-data", DisplayName: "测试密钥", CreatedAt: time.Now()},
		); err != nil {
			t.Fatal(err)
		}
		for _, protocol := range credentials.SupportedProtocols() {
			if _, err := engine.Insert(&models.ProtocolCredential{UserID: user.ID, Protocol: protocol,
				Enabled: true, CodeHash: hash, CodeVersion: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return engine, users
}

func assertResetCredentialState(t *testing.T, engine *xorm.Engine, userID int, revoked bool) {
	t.Helper()
	want := int64(1)
	if revoked {
		want = 0
	}
	for _, query := range []struct {
		bean  any
		where string
		count int64
	}{
		{&models.SecurityQuestion{}, "user_id = ?", want},
		{&models.RecoveryCode{}, "user_id = ? AND used_at IS NULL", want},
		{&models.WebAuthnCredential{}, "user_id = ? AND revoked_at IS NULL", want},
		{&models.ProtocolCredential{}, "user_id = ? AND revoked_at IS NULL", 3 * want},
		{&models.ClientCredential{}, "user_id = ? AND revoked_at IS NULL", want},
	} {
		count, err := engine.Where(query.where, userID).Count(query.bean)
		if err != nil || count != query.count {
			t.Fatalf("用户 %d 的 %T 有效数量 = %d，预期 %d，错误：%v", userID, query.bean, count, query.count, err)
		}
	}
}

func assertResetUserUnchanged(t *testing.T, engine *xorm.Engine, original models.User) {
	t.Helper()
	var actual models.User
	found, err := engine.ID(original.ID).Get(&actual)
	if err != nil || !found {
		t.Fatalf("读取原用户失败：%v", err)
	}
	if actual.Password != original.Password || actual.SessionVersion != original.SessionVersion ||
		actual.SecurityVersion != original.SecurityVersion || actual.MustChangePassword != original.MustChangePassword ||
		actual.PasswordChangedAt != nil {
		t.Fatal("失败重置或其他用户的密码与安全状态被修改")
	}
	assertResetCredentialState(t, engine, original.ID, false)
}

func testIntegrationControlSocketDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if len(dir)+len("/control.sock") >= 90 {
		shortDir, err := os.MkdirTemp("/tmp", "smail-*")
		if err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
			return shortDir
		}
	}
	return dir
}

func TestUnixResetCommitsPasswordAndRevokesAllProtocolCodes(t *testing.T) {
	engine, users := resetIntegrationDatabase(t)
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range credentials.SupportedProtocols() {
		if matched, err := service.CheckProtocolCode(users[0].ID, protocol, integrationOldPassword); err != nil || !matched {
			t.Fatalf("重置前协议 %s 的凭据不可用：%v", protocol, err)
		}
		if matched, err := service.CheckClientOrProtocolCode(users[0].ID, protocol, credentials.CleanAuthCode(integrationOldPassword)); err != nil || !matched {
			t.Fatalf("重置前协议 %s 的客户端码不可用：%v", protocol, err)
		}
	}
	path := filepath.Join(testIntegrationControlSocketDir(t), "control.sock")
	// 隔离夹具仅信任测试进程的 UID；不改变生产入口只信任 root 的配置。
	server, err := ListenUnix(path, []uint32{uint32(os.Geteuid())})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done := make(chan error, 1)
	invalidated := make(chan bool, 1)
	go func() {
		done <- server.Serve(ctx, testResetHandler(func(value string) error {
			return ResetSuperAdmin(engine, value, func(id int, account string) {
				var committed models.User
				found, err := engine.ID(id).Get(&committed)
				invalidated <- err == nil && found && id == users[0].ID && account == users[0].Account &&
					committed.SessionVersion == users[0].SessionVersion+1
			})
		}), NewSessionGuard(time.Minute))
	}()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("隔离控制服务退出失败：%v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("隔离控制服务没有退出")
		}
	})
	conn, err := DialControl(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := SendReset(ctx, conn, integrationNewPassword); err != nil {
		t.Fatal(err)
	}
	select {
	case committed := <-invalidated:
		if !committed {
			t.Fatal("运行态撤销未在目标用户事务提交后执行")
		}
	default:
		t.Fatal("未执行运行态撤销")
	}
	var updated models.User
	if found, err := engine.ID(users[0].ID).Get(&updated); err != nil || !found {
		t.Fatalf("读取更新后的管理员失败：%v", err)
	}
	if !password.Verify(updated.Password, integrationNewPassword) || password.Verify(updated.Password, integrationOldPassword) {
		t.Fatal("新旧密码验证结果错误")
	}
	if updated.SessionVersion != 4 || updated.SecurityVersion != 6 || updated.MustChangePassword != 0 || updated.PasswordChangedAt == nil {
		t.Fatal("密码重置的会话版本、安全版本或密码状态未提交")
	}
	assertResetCredentialState(t, engine, users[0].ID, true)
	assertResetUserUnchanged(t, engine, users[1])
	for _, protocol := range credentials.SupportedProtocols() {
		for index, user := range users {
			matched, err := service.CheckProtocolCode(user.ID, protocol, integrationOldPassword)
			if err != nil || matched != (index == 1) {
				t.Fatalf("重置后用户 %d 协议 %s 的凭据验证结果错误：%v", user.ID, protocol, err)
			}
			matched, err = service.CheckClientOrProtocolCode(user.ID, protocol, credentials.CleanAuthCode(integrationOldPassword))
			if err != nil || matched != (index == 1) {
				t.Fatalf("重置后用户 %d 协议 %s 的客户端码验证结果错误：%v", user.ID, protocol, err)
			}
		}
	}
}

func TestResetSuperAdminRollsBackEveryRevocationStage(t *testing.T) {
	for _, stage := range []struct{ table, operation string }{
		{"recovery_code", "UPDATE"}, {"security_question", "DELETE"},
		{"webauthn_credential", "UPDATE"}, {"protocol_credential", "UPDATE"},
		{"client_credential", "UPDATE"},
	} {
		t.Run(stage.table, func(t *testing.T) {
			engine, users := resetIntegrationDatabase(t)
			// 在真实事务的不同阶段拒绝写入，包含前面多步已成功的最终撤销阶段。
			query := "CREATE TRIGGER reject_revocation BEFORE " + stage.operation + " ON " + stage.table +
				" BEGIN SELECT RAISE(ABORT, 'injected revocation failure'); END"
			if _, err := engine.Exec(query); err != nil {
				t.Fatal(err)
			}
			invalidated := false
			err := ResetSuperAdmin(engine, integrationNewPassword, func(int, string) { invalidated = true })
			if err == nil || !strings.Contains(err.Error(), "injected revocation failure") || invalidated {
				t.Fatalf("没有命中指定撤销故障，或提前撤销运行态：%v", err)
			}
			for _, user := range users {
				assertResetUserUnchanged(t, engine, user)
			}
		})
	}
}

func TestResetSuperAdminRejectsAmbiguousTargetWithoutChanges(t *testing.T) {
	engine, users := resetIntegrationDatabase(t)
	users[1].Role, users[1].IsAdmin = "super_admin", 1
	if _, err := engine.ID(users[1].ID).Cols("role", "is_admin").Update(&users[1]); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	err := ResetSuperAdmin(engine, integrationNewPassword, func(int, string) { invalidated = true })
	if !errors.Is(err, ErrSuperAdminAmbiguous) || invalidated {
		t.Fatal("多个超级管理员时未安全拒绝")
	}
	for _, user := range users {
		assertResetUserUnchanged(t, engine, user)
	}
}
