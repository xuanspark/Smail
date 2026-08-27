package models

import "time"

// VerificationCleanupState 保存清理调度状态和租约，不保存验证码相关秘密。
type VerificationCleanupState struct {
	StateKey     string    `xorm:"state_key varchar(64) notnull pk" json:"-"`
	NextRunAt    time.Time `xorm:"next_run_at datetime notnull index" json:"-"`
	LeaseUntil   time.Time `xorm:"lease_until datetime notnull" json:"-"`
	LeaseToken   string    `xorm:"lease_token char(64) notnull" json:"-"`
	FailureCount int       `xorm:"failure_count int notnull default(0)" json:"-"`
	UpdatedAt    time.Time `xorm:"updated_at datetime notnull" json:"-"`
}

func (VerificationCleanupState) TableName() string {
	return "verification_cleanup_state"
}
