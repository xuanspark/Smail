package models

import "time"

// PluginSecret 保存插件全局敏感字段的密文；明文和主密钥均不得写入数据库。
type PluginSecret struct {
	ID            int       `xorm:"pk autoincr 'id'"`
	PluginID      string    `xorm:"varchar(64) notnull unique(plugin_secret_field) 'plugin_id'"`
	FieldName     string    `xorm:"varchar(64) notnull unique(plugin_secret_field) 'field_name'"`
	Ciphertext    string    `xorm:"text notnull 'ciphertext'" json:"-"`
	KeyVersion    uint32    `xorm:"notnull 'key_version'" json:"-"`
	RecordVersion uint64    `xorm:"notnull default(1) 'record_version'" json:"record_version"`
	CreatedAt     time.Time `xorm:"created 'created_at'" json:"-"`
	UpdatedAt     time.Time `xorm:"updated 'updated_at'" json:"-"`
}

func (PluginSecret) TableName() string {
	return "plugin_secret"
}

// PluginUserSetting 保存用户范围的加密插件设置，不能保存全局插件密钥。
type PluginUserSetting struct {
	ID                 int       `xorm:"pk autoincr 'id'"`
	PluginID           string    `xorm:"varchar(64) notnull unique(plugin_user_setting_owner) 'plugin_id'"`
	UserID             int       `xorm:"notnull unique(plugin_user_setting_owner) index 'user_id'"`
	SchemaVersion      uint32    `xorm:"notnull 'schema_version'" json:"-"`
	SettingsCiphertext string    `xorm:"text notnull 'settings_ciphertext'" json:"-"`
	SettingsHash       string    `xorm:"varchar(128) notnull 'settings_hash'" json:"-"`
	Enabled            bool      `xorm:"notnull default(1) 'enabled'" json:"enabled"`
	Version            uint64    `xorm:"notnull default(1) 'version'" json:"version"`
	CreatedAt          time.Time `xorm:"created 'created_at'" json:"-"`
	UpdatedAt          time.Time `xorm:"updated 'updated_at'" json:"-"`
}

func (PluginUserSetting) TableName() string {
	return "plugin_user_setting"
}
