package response

import "git.pcux.cn/xuans/Smail/models"

type EmailResponseData struct {
	models.Email  `xorm:"extends"`
	IsRead        int8 `json:"is_read"`
	IsDeleted     int8 `json:"-"`
	UserStatus    int8 `xorm:"user_status" json:"-"`
	SerialNumber  int  `json:"serial_number"`
	UeId          int  `json:"ue_id"`
	IsBlacklisted bool `xorm:"-" json:"is_blacklisted"`
}

type UserEmailUIDData struct {
	models.UserEmail `xorm:"extends"`
	SerialNumber     int `json:"serial_number"`
}
