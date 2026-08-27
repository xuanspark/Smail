package models

import "time"

// VerificationRateLimit 保存限流计数，不保存验证码、挑战值或完整身份标识。
type VerificationRateLimit struct {
	ID            int        `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	DimensionKey  string     `xorm:"dimension_key char(64) notnull unique" json:"-"`
	DimensionType string     `xorm:"dimension_type varchar(32) notnull index" json:"-"`
	HourStartedAt *time.Time `xorm:"hour_started_at datetime null" json:"-"`
	HourCount     int        `xorm:"hour_count int notnull default(0)" json:"-"`
	DayStartedAt  *time.Time `xorm:"day_started_at datetime null" json:"-"`
	DayCount      int        `xorm:"day_count int notnull default(0)" json:"-"`
	LastIssuedAt  *time.Time `xorm:"last_issued_at datetime null" json:"-"`
	ExpiresAt     time.Time  `xorm:"expires_at datetime notnull index" json:"-"`
	UpdatedAt     time.Time  `xorm:"updated_at datetime notnull index" json:"-"`
}

func (VerificationRateLimit) TableName() string {
	return "verification_rate_limit"
}
