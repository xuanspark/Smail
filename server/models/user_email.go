package models

import "time"

type UserEmail struct {
	ID               int        `xorm:"id int unsigned not null pk autoincr"`
	UserID           int        `xorm:"user_id int not null index('idx_eid') index('idx_user_email_user_primary')"`
	EmailID          int        `xorm:"email_id not null index('idx_eid') index"`
	Address          string     `xorm:"address varchar(254) null" json:"address,omitempty"`
	AddressCanonical string     `xorm:"address_canonical varchar(254) null unique" json:"-"`
	IsPrimary        int8       `xorm:"is_primary tinyint(1) notnull default(0)" json:"is_primary"`
	VerifiedAt       *time.Time `xorm:"verified_at datetime null" json:"verified_at,omitempty"`
	CreatedAt        time.Time  `xorm:"created_at datetime created" json:"created_at"`
	UpdatedAt        time.Time  `xorm:"updated_at datetime updated" json:"updated_at"`
	IsRead           int8       `xorm:"is_read tinyint(1) comment('是否已读')" json:"is_read"`
	GroupId          int        `xorm:"group_id int notnull default(0) comment('分组id')'" json:"group_id"`
	Status           int8       `xorm:"status tinyint(4) notnull default(0) comment('0未发送或收件，1已发送，2发送失败，3删除 4草稿 5广告')" json:"status"` // 0未发送或收件，1已发送，2发送失败 3删除 4草稿箱(Drafts)  5骚扰邮件(Junk)
	Created          time.Time  `xorm:"create datetime created index('idx_create_time')"`
}

func (p UserEmail) TableName() string {
	return "user_email"
}
