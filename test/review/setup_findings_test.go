package setup

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	requestcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// 使用独立临时库和真实版本化迁移，不读取或改动开发服务数据库。
func TestAuditFirstAdministratorMustHaveSuperAdminRole(t *testing.T) {
	oldConfig, oldSnapshot := config.Instance, config.CurrentConfig()
	oldManager, oldDB := config.CurrentManager(), db.Instance
	t.Setenv("SMAIL_ENABLE_LEGACY_SYNC2", "0")
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "first-admin.db"),
	}
	config.PublishConfigSnapshot(config.Instance)
	config.SetRuntimeManager(nil)
	db.Instance = nil
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
		config.PublishConfigSnapshot(oldSnapshot)
		config.SetRuntimeManager(oldManager)
	})
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	if err := SetAdminPassword(&requestcontext.Context{}, "audit-admin", "AuditPass12"); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"created", "restarted"} {
		if stage == "restarted" {
			if err := db.Instance.Close(); err != nil {
				t.Fatal(err)
			}
			db.Instance = nil
			if err := db.Init(""); err != nil {
				t.Fatal(err)
			}
		}
		var user models.User
		found, err := db.Instance.Where("account = ?", "audit-admin").Get(&user)
		if err != nil || !found {
			t.Fatalf("读取初始化账户失败: found=%t err=%v", found, err)
		}
		canReadConfig, canManageRole := false, false
		for _, capability := range access.Capabilities(user.EffectiveRole()) {
			canReadConfig = canReadConfig || capability == access.CapabilityConfigRead
			canManageRole = canManageRole || capability == access.CapabilityRoleManage
		}
		t.Logf("stage=%s is_admin=%d role=%q effective_role=%q config_read=%t role_manage=%t", stage, user.IsAdmin, user.Role, user.EffectiveRole(), canReadConfig, canManageRole)
		if user.EffectiveRole() != access.RoleSuperAdmin || !canReadConfig || !canManageRole {
			t.Errorf("%s: 初始化管理员必须获得超级管理员角色和管理能力", stage)
		}
	}
}
