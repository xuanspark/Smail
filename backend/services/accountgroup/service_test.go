package accountgroup

import (
	"testing"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestCanAccessExcludesSuperAdminFromAdminScope(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	if err := engine.Sync2(new(models.User), new(models.AccountGroupMember)); err != nil {
		t.Fatal(err)
	}

	superAdmin := models.User{Account: "superadmin", Name: "超级管理员", Role: string(access.RoleSuperAdmin)}
	admin := models.User{Account: "admin", Name: "分组管理员", Role: string(access.RoleAdmin), IsAdmin: 1}
	member := models.User{Account: "member", Name: "普通用户", Role: string(access.RoleUser)}
	if _, err := engine.Insert(&superAdmin, &admin, &member); err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Insert(
		&models.AccountGroupMember{UserID: admin.ID, GroupID: 1},
		&models.AccountGroupMember{UserID: member.ID, GroupID: 1},
	); err != nil {
		t.Fatal(err)
	}

	if CanAccess(engine, admin.ID, access.RoleAdmin, superAdmin.ID) {
		t.Fatal("管理员不应访问超级管理员账户")
	}
	if !CanAccess(engine, admin.ID, access.RoleAdmin, member.ID) {
		t.Fatal("管理员应访问同组普通用户账户")
	}
}
