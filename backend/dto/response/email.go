package response

import (
	"encoding/json"

	"git.pcux.cn/xuans/Smail/models"
)

type EmailResponseData struct {
	models.Email  `xorm:"extends"`
	IsRead        int8 `json:"is_read"`
	IsDeleted     int8 `json:"-"`
	UserStatus    int8 `xorm:"user_status" json:"-"`
	SerialNumber  int  `json:"serial_number"`
	UeId          int  `json:"ue_id"`
	IsBlacklisted bool `xorm:"-" json:"is_blacklisted"`
}

func (d *EmailResponseData) MarshalJSON() ([]byte, error) {
	data, err := d.Email.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{
		"is_read":        d.IsRead,
		"serial_number":  d.SerialNumber,
		"ue_id":          d.UeId,
		"is_blacklisted": d.IsBlacklisted,
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[name] = encoded
	}
	return json.Marshal(fields)
}

type UserEmailUIDData struct {
	models.UserEmail `xorm:"extends"`
	SerialNumber     int `json:"serial_number"`
}
