package models

import "time"

type ExternalEmail struct {
	ID                int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID            int        `xorm:"user_id bigint not null index notnull" json:"-"`
	EmailCiphertext   string     `xorm:"email_ciphertext varchar(1024) notnull" json:"-"`
	EmailFingerprint  string     `xorm:"email_fingerprint char(64) notnull unique index" json:"-"`
	EmailType         string     `xorm:"email_type varchar(16) notnull" json:"email_type"`
	Status            string     `xorm:"status varchar(16) notnull" json:"status"`
	VerifiedAt        *time.Time `xorm:"verified_at datetime null" json:"verified_at,omitempty"`
	CredentialVersion int        `xorm:"credential_version int notnull default(1)" json:"-"`
	CreatedAt         time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt         time.Time  `xorm:"updated_at datetime updated notnull" json:"updated_at"`
}

const (
	ExternalEmailTypePrimary    = "primary"
	ExternalEmailTypeBackup     = "backup"
	ExternalEmailStatusPending  = "pending"
	ExternalEmailStatusVerified = "verified"
	ExternalEmailStatusDisabled = "disabled"
)

func (ExternalEmail) TableName() string {
	return "external_email"
}
