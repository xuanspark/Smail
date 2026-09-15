package db

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

func TestSuperAdminInvariantDoesNotMutateAccounts(t *testing.T) {
	if !errors.Is(ValidateSuperAdmin(nil), ErrSuperAdminInvariant) {
		t.Fatal("未初始化数据库没有被拒绝")
	}
	for _, tc := range []struct {
		name  string
		users []models.User
		valid bool
	}{
		{"empty", nil, false},
		{"ordinary", []models.User{{Role: "user"}}, false},
		{"explicit-user", []models.User{{Role: "user", IsAdmin: 1}}, false},
		{"single", []models.User{{Role: "super_admin"}}, true},
		{"disabled", []models.User{{Role: "super_admin", Disabled: 1}}, false},
		{"duplicate", []models.User{{Role: "super_admin"}, {Role: "super_admin", Disabled: 1}}, false},
		{"legacy", []models.User{{Role: "legacy", IsAdmin: 1}}, true},
		{"mixed-duplicate", []models.User{{Role: "super_admin"}, {Role: "legacy", IsAdmin: 1}}, false},
		{"multiple-admins", []models.User{{Role: "super_admin"}, {Role: "admin", IsAdmin: 1}, {Role: "admin", IsAdmin: 1}, {Role: "user"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "accounts.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			if err := engine.Sync2(new(models.User)); err != nil {
				t.Fatal(err)
			}
			for index, user := range tc.users {
				user.Account, user.Name, user.Password = string(rune('a'+index)), "test", "unused"
				if _, err := engine.Insert(&user); err != nil {
					t.Fatal(err)
				}
			}
			var before, after []models.User
			if err := engine.OrderBy("id").Find(&before); err != nil {
				t.Fatal(err)
			}
			err = ValidateSuperAdmin(engine)
			if (err == nil) != tc.valid || (!tc.valid && !errors.Is(err, ErrSuperAdminInvariant)) {
				t.Fatalf("唯一性检查不符合预期：%v", err)
			}
			if err := engine.OrderBy("id").Find(&after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("检查意外修改了账号")
			}
		})
	}
}
