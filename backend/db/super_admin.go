package db

import (
	"errors"
	"fmt"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

var ErrSuperAdminInvariant = errors.New("必须有且仅有一个启用的超级管理员，请在部署端核查账号数据")

// ValidateSuperAdmin 在初始化完成和业务启动前检查唯一管理员，不自动提权或降级任何账号。
func ValidateSuperAdmin(engine *xorm.Engine) error {
	if engine == nil {
		return fmt.Errorf("%w：数据库尚未初始化", ErrSuperAdminInvariant)
	}
	var users []models.User
	if err := engine.Cols("id", "disabled").Where(models.SuperAdminPredicate).Limit(2).Find(&users); err != nil {
		return fmt.Errorf("读取超级管理员状态失败：%w", err)
	}
	if len(users) != 1 || users[0].Disabled != 0 {
		return ErrSuperAdminInvariant
	}
	return nil
}
