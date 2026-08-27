package models

import "time"

// VerificationEvent 保存邮箱验证码运营数据，不保存验证码、挑战值或校验值。
type VerificationEvent struct {
	ID                 int       `xorm:"id bigint unsigned not null pk autoincr" json:"-"`
	UserID             int       `xorm:"user_id bigint not null default(0) index" json:"-"`
	Purpose            string    `xorm:"purpose varchar(32) notnull index" json:"purpose"`
	Action             string    `xorm:"action varchar(32) notnull index" json:"action"`
	Outcome            string    `xorm:"outcome varchar(16) notnull index" json:"outcome"`
	AccountFingerprint string    `xorm:"account_fingerprint char(80) null" json:"-"`
	EmailFingerprint   string    `xorm:"email_fingerprint char(80) null" json:"-"`
	IPFingerprint      string    `xorm:"ip_fingerprint char(80) null" json:"-"`
	ExpiresAt          time.Time `xorm:"expires_at datetime notnull index" json:"-"`
	RetentionClass     string    `xorm:"retention_class varchar(16) notnull index" json:"-"`
	InvestigationHold  bool      `xorm:"investigation_hold bool notnull default(0) index" json:"-"`
	CreatedAt          time.Time `xorm:"created_at datetime notnull index" json:"created_at"`
}

func (VerificationEvent) TableName() string {
	return "verification_event"
}

const (
	VerificationActionIssue   = "issue"
	VerificationActionVerify  = "verify"
	VerificationActionCleanup = "cleanup"

	VerificationOutcomeSuccess = "success"
	VerificationOutcomeFailure = "failure"
	VerificationOutcomeBlocked = "blocked"
)
