package auth

import (
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestAuthenticateUsesArgon2idOnly(t *testing.T) {
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "auth.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Instance.Close()
	})

	user := models.User{Account: "test-user", Name: "测试用户", IsAdmin: 1}
	if err := SetPassword(&user, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(user.Password, "$argon2id$") || !password.Verify(user.Password, "CorrectP12") {
		t.Fatal("测试密码哈希格式或校验失败")
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}

	var loaded models.User
	if _, err := db.Instance.Where("account = ?", "test-user").Get(&loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.ID == 0 || !strings.HasPrefix(loaded.Password, "$argon2id$") {
		t.Fatalf("测试用户未正确写入")
	}
	if _, err := Authenticate("test-user", "CorrectP12"); err != nil {
		t.Fatalf("新密码认证失败: %v", err)
	}
	if _, err := Authenticate("test-user", "WrongPass12"); err == nil {
		t.Fatal("错误密码不应认证成功")
	}
	if _, err := Authenticate("test-user", ""); err == nil {
		t.Fatal("空密码不应认证成功")
	}
}

func TestAuthenticateRejectsLegacyHash(t *testing.T) {
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "legacy.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Instance.Close()
	})

	user := models.User{Account: "legacy-user", Name: "旧算法用户", Password: "4f3b2f8f3f8f3f8f3f8f3f8f3f8f3f8f"}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := Authenticate("legacy-user", "legacy-password"); err == nil {
		t.Fatal("旧算法哈希不应认证成功")
	}
}

func TestHasAuthRequiresMailboxOwnershipForAdministrators(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "mailbox-auth.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
	})

	email := models.Email{MsgID: "mailbox-auth@example.com"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: 1, EmailID: email.Id}); err != nil {
		t.Fatal(err)
	}
	if !HasAuth(&context.Context{UserID: 1}, &email) {
		t.Fatal("mailbox owner was rejected")
	}
	if HasAuth(&context.Context{UserID: 2, IsAdmin: true}, &email) {
		t.Fatal("administrator bypassed mailbox ownership")
	}
}
