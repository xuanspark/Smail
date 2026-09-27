package models

import "time"

// SecurityQuestion 保存密保问题和 Argon2id 答案哈希，不保存答案明文。
type SecurityQuestion struct {
	ID         int       `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID     int       `xorm:"user_id bigint not null unique" json:"-"`
	Question   string    `xorm:"question varchar(255) notnull" json:"question"`
	AnswerHash string    `xorm:"answer_hash varchar(255) notnull" json:"-"`
	Version    int       `xorm:"version int notnull default(1)" json:"-"`
	CreatedAt  time.Time `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt  time.Time `xorm:"updated_at datetime updated notnull" json:"updated_at"`
}

func (SecurityQuestion) TableName() string { return "security_question" }

// RecoveryCode 保存一次性离线恢复码的 Argon2id 哈希，UsedAt 非空表示已经消费或撤销。
type RecoveryCode struct {
	ID             int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID         int        `xorm:"user_id bigint not null index" json:"-"`
	CodeHash       string     `xorm:"code_hash varchar(255) notnull" json:"-"`
	CodeCiphertext string     `xorm:"code_ciphertext varchar(512) null" json:"-"`
	CodeVersion    int        `xorm:"code_version int notnull default(1)" json:"-"`
	UsedAt         *time.Time `xorm:"used_at datetime null index" json:"-"`
	CreatedAt      time.Time  `xorm:"created_at datetime created notnull index" json:"created_at"`
}

func (RecoveryCode) TableName() string { return "recovery_code" }

// WebAuthnCredential 保存 WebAuthn 公钥凭据的脱敏索引和加密数据。
// CredentialData 是由独立凭据密钥保护的序列化凭据，不允许保存明文公钥材料。
type WebAuthnCredential struct {
	ID             int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID         int        `xorm:"user_id bigint not null index" json:"-"`
	CredentialID   string     `xorm:"credential_id varchar(512) notnull unique" json:"-"`
	CredentialData string     `xorm:"credential_data text notnull" json:"-"`
	DisplayName    string     `xorm:"display_name varchar(128) notnull" json:"display_name"`
	Discoverable   bool       `xorm:"discoverable notnull default(0)" json:"discoverable"`
	CreatedAt      time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	LastUsedAt     *time.Time `xorm:"last_used_at datetime null" json:"last_used_at"`
	RevokedAt      *time.Time `xorm:"revoked_at datetime null index" json:"-"`
}

func (WebAuthnCredential) TableName() string { return "webauthn_credential" }

type TOTPAuthenticator struct {
	ID                int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID            int        `xorm:"user_id bigint not null unique" json:"-"`
	SecretCiphertext  string     `xorm:"secret_ciphertext text notnull" json:"-"`
	CredentialVersion int        `xorm:"credential_version int notnull default(1)" json:"-"`
	Enabled           bool       `xorm:"enabled notnull default(1)" json:"enabled"`
	ConfirmedAt       *time.Time `xorm:"confirmed_at datetime null" json:"confirmed_at,omitempty"`
	LastUsedStep      int64      `xorm:"last_used_step bigint notnull default(0)" json:"-"`
	CreatedAt         time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt         time.Time  `xorm:"updated_at datetime updated notnull" json:"updated_at"`
	RevokedAt         *time.Time `xorm:"revoked_at datetime null index" json:"revoked_at,omitempty"`
}

func (TOTPAuthenticator) TableName() string { return "totp_authenticator" }
