package models

import "time"

// ProtocolCredential 保存单个用户的第三方邮件协议开关和授权码哈希。
type ProtocolCredential struct {
	ID          int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID      int        `xorm:"user_id bigint not null" json:"-"`
	Protocol    string     `xorm:"protocol varchar(16) notnull" json:"protocol"`
	Enabled     bool       `xorm:"enabled notnull default(0)" json:"enabled"`
	CodeHash    string     `xorm:"code_hash varchar(255) notnull" json:"-"`
	CodeVersion int        `xorm:"code_version int notnull default(1)" json:"-"`
	CreatedAt   time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt   time.Time  `xorm:"updated_at datetime updated notnull" json:"updated_at"`
	LastUsedAt  *time.Time `xorm:"last_used_at datetime null" json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `xorm:"revoked_at datetime null index" json:"revoked_at,omitempty"`
}

func (ProtocolCredential) TableName() string { return "protocol_credential" }
