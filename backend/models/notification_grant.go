package models

// NotificationGrant 与业务数据共用数据库；摘要用于认证，密文仅供身份验证后查看。
type NotificationGrant struct {
	UserID          int    `xorm:"user_id pk" json:"user_id"`
	Enabled         int    `xorm:"enabled" json:"-"`
	CodeVersion     int    `xorm:"code_version" json:"version"`
	CodeHash        string `xorm:"code_hash" json:"-"`
	CodeCiphertext  string `xorm:"code_ciphertext varchar(512) null" json:"-"`
	SessionVersion  int    `xorm:"session_version" json:"-"`
	SecurityVersion int    `xorm:"security_version" json:"-"`
	MinuteLimit     int    `xorm:"minute_limit" json:"minute_limit"`
	DayLimit        int    `xorm:"day_limit" json:"day_limit"`
	MinuteKey       string `xorm:"minute_key" json:"-"`
	MinuteCount     int    `xorm:"minute_count" json:"-"`
	DayKey          string `xorm:"day_key" json:"-"`
	DayCount        int    `xorm:"day_count" json:"-"`
	SenderName      string `xorm:"sender_name varchar(64) null" json:"sender_name"`
}

func (NotificationGrant) TableName() string { return "notification_grant" }
