package models

import "time"

// ClientCredential 保存用户为特定客户端（如 iPhone、Foxmail 等）创建的专属授权码
type ClientCredential struct {
	ID             int        `xorm:"id bigint unsigned not null pk autoincr" json:"id"`
	UserID         int        `xorm:"user_id bigint not null index" json:"user_id"`
	ClientName     string     `xorm:"client_name varchar(64) notnull" json:"client_name"`
	CodeHash       string     `xorm:"code_hash varchar(255) notnull" json:"-"`
	CodeCiphertext string     `xorm:"code_ciphertext varchar(512) null" json:"-"`
	CodeVersion    int        `xorm:"code_version int notnull default(1)" json:"code_version"`
	CreatedAt      time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt      time.Time  `xorm:"updated_at datetime updated notnull" json:"updated_at"`
	LastUsedAt     *time.Time `xorm:"last_used_at datetime null" json:"last_used_at,omitempty"`
	RevokedAt      *time.Time `xorm:"revoked_at datetime null index" json:"revoked_at,omitempty"`
}

func (ClientCredential) TableName() string { return "client_credential" }
