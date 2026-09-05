package models

import (
	"time"

	"git.pcux.cn/xuans/Smail/services/access"
)

type User struct {
	ID             int    `xorm:"id unsigned int not null pk autoincr"`
	Account        string `xorm:"varchar(20) notnull unique comment('账号登陆名')"`
	Name           string `xorm:"varchar(10) notnull comment('用户名')"`
	Password       string `xorm:"varchar(255) notnull comment('Argon2id 密码哈希')" json:"-"`
	Disabled       int    `xorm:"disabled unsigned int not null default(0) comment('0启用，1禁用')"`
	Role           string `xorm:"varchar(20) not null default('user') comment('用户角色')"`
	IsAdmin        int    `xorm:"is_admin unsigned int not null default(0) comment('0不是管理员，1是管理员')"`
	SessionVersion int    `xorm:"session_version unsigned int not null default(1)" json:"-"`
	SecurityVersion int       `xorm:"security_version unsigned int not null default(1)" json:"-"`
	MustChangePassword int    `xorm:"must_change_password unsigned int not null default(0)" json:"must_change_password"`
	PasswordChangedAt  *time.Time `xorm:"password_changed_at datetime null" json:"-"`
	SecuritySetupAcknowledged int `xorm:"security_setup_acknowledged not null default(0)" json:"-"`
}

// EffectiveRole 返回新角色字段，兼容旧版本只保存 IsAdmin 的账户。
func (p User) EffectiveRole() access.Role {
	role := access.Role(p.Role)
	if role.Valid() {
		return role
	}
	if p.IsAdmin == 1 {
		return access.RoleSuperAdmin
	}
	return access.RoleUser
}

// ApplyRole 同步角色字段和旧版管理员标记，避免兼容期出现权限漂移。
func (p *User) ApplyRole(role access.Role) {
	if p == nil || !role.Valid() {
		return
	}
	p.Role = string(role)
	if role == access.RoleSuperAdmin || role == access.RoleAdmin {
		p.IsAdmin = 1
	} else {
		p.IsAdmin = 0
	}
}

func (p User) TableName() string {
	return "user"
}
