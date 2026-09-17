package models

type OutboundDelivery struct {
	ID          int64  `xorm:"id pk autoincr"`
	EmailID     int    `xorm:"email_id notnull index"`
	Recipient   string `xorm:"varchar(320) notnull"`
	State       string `xorm:"varchar(24) notnull"`
	Attempts    int    `xorm:"notnull"`
	NextAttempt int64  `xorm:"notnull"`
	LastError   string `xorm:"text"`
}

func (OutboundDelivery) TableName() string { return "outbound_delivery" }
