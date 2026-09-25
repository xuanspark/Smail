package models

// AccountGroup 是账号管理分组，与邮箱文件夹分开；编号零为隐式默认分组。
type AccountGroup struct {
	ID   int    `xorm:"id pk autoincr" json:"id"`
	Name string `xorm:"varchar(64) notnull unique" json:"name"`
}

func (AccountGroup) TableName() string { return "account_group" }

type AccountGroupMember struct {
	UserID  int `xorm:"user_id pk" json:"user_id"`
	GroupID int `xorm:"group_id pk" json:"group_id"`
}

func (AccountGroupMember) TableName() string { return "account_group_member" }
