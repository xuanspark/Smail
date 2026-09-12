package models

import "time"

// UserContact 保存单个用户的历史写信收件人，用于写信页自动补全与联系人管理
type UserContact struct {
	ID         int       `xorm:"id bigint unsigned not null pk autoincr" json:"id"`
	UserID     int       `xorm:"user_id bigint not null" json:"-"`
	Email      string    `xorm:"email varchar(255) notnull" json:"email"`
	Name       string    `xorm:"name varchar(100) notnull default('')" json:"name"`
	LastUsedAt time.Time `xorm:"last_used_at datetime notnull" json:"last_used_at"`
}

func (UserContact) TableName() string { return "user_contact" }

// UserTrustedSender 保存单个用户手动标记信任的发件人白名单，免除安全警告
type UserTrustedSender struct {
	ID        int       `xorm:"id bigint unsigned not null pk autoincr" json:"id"`
	UserID    int       `xorm:"user_id bigint not null" json:"-"`
	Email     string    `xorm:"email varchar(255) notnull" json:"email"`
	CreatedAt time.Time `xorm:"created_at datetime created notnull" json:"created_at"`
}

func (UserTrustedSender) TableName() string { return "user_trusted_sender" }
