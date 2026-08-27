package setup

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestSetAdminPasswordValidatesAndPreventsDuplicates(t *testing.T) {
	oldConfig := config.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "setup.db")}
	t.Cleanup(func() { config.Instance = oldConfig })
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Instance.Close() })

	ctx := &context.Context{}
	if err := SetAdminPassword(ctx, ".invalid", "StrongPass123"); err == nil {
		t.Fatal("invalid administrator account was accepted")
	}
	if err := SetAdminPassword(ctx, "admin-user", "short"); err == nil {
		t.Fatal("short administrator password was accepted")
	}
	if err := SetAdminPassword(ctx, "admin-user", "StrongPass123"); err != nil {
		t.Fatal(err)
	}
	if err := SetAdminPassword(ctx, "second-admin", "StrongPass456"); err == nil {
		t.Fatal("duplicate administrator initialization was accepted")
	}

	var user models.User
	if _, err := db.Instance.Where("account = ?", "admin-user").Get(&user); err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 || user.IsAdmin != 1 || !password.Verify(user.Password, "StrongPass123") {
		t.Fatal("administrator was not initialized correctly")
	}
}
