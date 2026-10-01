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

func TestMembershipScopeGuardLimitsAdminToOwnedGroups(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	if err := engine.Sync2(new(models.User), new(models.AccountGroup), new(models.AccountGroupMember)); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Account: "admin", Name: "分组管理员", Role: string(access.RoleAdmin), IsAdmin: 1}
	superAdmin := models.User{Account: "superadmin", Name: "超级管理员", Role: string(access.RoleSuperAdmin)}
	if _, err := engine.Insert(&admin, &superAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(
		&models.AccountGroup{ID: 1, Name: "一组"},
		&models.AccountGroup{ID: 2, Name: "二组"},
		&models.AccountGroupMember{UserID: admin.ID, GroupID: 1},
	); err != nil {
		t.Fatal(err)
	}

	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err := MembershipScopeGuard(admin.ID, access.RoleAdmin, []int{1})(tx); err != nil {
		t.Fatalf("管理员应能管理所属分组：%v", err)
	}
	if err := MembershipScopeGuard(admin.ID, access.RoleAdmin, []int{2})(tx); err != ErrDenied {
		t.Fatalf("管理员不应管理其他分组，得到：%v", err)
	}
	if err := MembershipScopeGuard(superAdmin.ID, access.RoleSuperAdmin, []int{2})(tx); err != nil {
		t.Fatalf("超级管理员应能管理全部分组：%v", err)
	}
}

func TestMembershipScopeGuardUsesDefaultGroupWhenAdminHasNoMembership(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	if err := engine.Sync2(new(models.User), new(models.AccountGroupMember)); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Account: "admin", Name: "默认分组管理员", Role: string(access.RoleAdmin), IsAdmin: 1}
	if _, err := engine.Insert(&admin); err != nil {
		t.Fatal(err)
	}

	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err := MembershipScopeGuard(admin.ID, access.RoleAdmin, []int{0})(tx); err != nil {
		t.Fatalf("无明确分组的管理员应能管理默认分组：%v", err)
	}
	if err := MembershipScopeGuard(admin.ID, access.RoleAdmin, []int{1})(tx); err != ErrDenied {
		t.Fatalf("无明确分组的管理员不应管理其他分组，得到：%v", err)
	}
}

func TestValidateMembershipKeepsAdminMultiGroupAndUserSingleGroupRules(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(new(models.AccountGroup)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.AccountGroup{ID: 1, Name: "一组"}, &models.AccountGroup{ID: 2, Name: "二组"}); err != nil {
		t.Fatal(err)
	}

	tx := engine.NewSession()
	defer tx.Close()
	if ids, err := ValidateMembership(tx, access.RoleAdmin, []int{1, 2}); err != nil || len(ids) != 2 {
		t.Fatalf("管理员应允许多个分组，ids=%v err=%v", ids, err)
	}
	if _, err := ValidateMembership(tx, access.RoleUser, []int{1, 2}); err != ErrInvalid {
		t.Fatalf("普通用户不应允许多个分组，得到：%v", err)
	}
}

func TestSetScopedMembershipPreservesGroupsOutsideAdminScope(t *testing.T) {
	engine, err := xorm.NewEngine(`sqlite`, `:memory:`)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(new(models.User), new(models.AccountGroup), new(models.AccountGroupMember)); err != nil {
		t.Fatal(err)
	}
	target := models.User{Account: `target`, Name: `Target`, Role: string(access.RoleUser), SessionVersion: 1, SecurityVersion: 1}
	if _, err := engine.Insert(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.AccountGroup{ID: 1, Name: `visible`}, &models.AccountGroup{ID: 2, Name: `hidden`}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.AccountGroupMember{UserID: target.ID, GroupID: 1}, &models.AccountGroupMember{UserID: target.ID, GroupID: 2}); err != nil {
		t.Fatal(err)
	}
	allow := func(*xorm.Session) error { return nil }
	if err := SetScopedMembership(engine, target, []int{0}, []int{0, 1}, allow); err != nil {
		t.Fatal(err)
	}
	rows := make([]models.AccountGroupMember, 0)
	if err := engine.Where(`user_id = ?`, target.ID).Asc(`group_id`).Find(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].GroupID != 0 || rows[1].GroupID != 2 {
		t.Fatalf(`scoped membership changed an inaccessible group: %+v`, rows)
	}
}
