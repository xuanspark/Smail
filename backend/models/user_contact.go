package models

import (
	"fmt"
	"strings"
	"time"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// UserContact 保存单个用户的厇史写信收仴人，用于写信页自动补全与遗系人管理
type UserContact struct {
	ID         int       `xorm:"id bigint unsigned not null pk autoincr" json:"id"`
	UserID     int       `xorm:"user_id bigint not null" json:"-"`
	Email      string    `xorm:"email varchar(255) notnull" json:"email"`
	Name       string    `xorm:"name varchar(100) notnull default('')" json:"name"`
	LastUsedAt time.Time `xorm:"last_used_at datetime notnull" json:"last_used_at"`
}

func (UserContact) TableName() string { return "user_contact" }

const (
	FilterTypeWhitelist = "whitelist"
	FilterTypeBlacklist = "blacklist"
)

// UserTrustedSender 保存单个用户标记的白名单（信任）与黑名单发件人，控制收信安全警告与拒信/风险标议
type UserTrustedSender struct {
	ID         int        `xorm:"id bigint unsigned not null pk autoincr" json:"id"`
	UserID     int        `xorm:"user_id bigint not null" json:"-"`
	Email      string     `xorm:"email varchar(255) notnull" json:"email"`
	FilterType string     `xorm:"filter_type varchar(20) notnull default('whitelist')" json:"filter_type"`
	CreatedAt  time.Time  `xorm:"created_at datetime created notnull" json:"created_at"`
	UpdatedAt  *time.Time `xorm:"updated_at datetime updated null" json:"updated_at,omitempty"`
}

func (UserTrustedSender) TableName() string { return "user_trusted_sender" }

func BlacklistedRecipientIDs(session *xorm.Session, userIDs []int, addresses ...string) (map[int]bool, error) {
	blocked := make(map[int]bool)
	if session == nil {
		return nil, fmt.Errorf("sender filter database session is unavailable")
	}
	var normalized []string
	for _, address := range addresses {
		if address = strings.ToLower(strings.TrimSpace(address)); address != "" {
			normalized = append(normalized, address)
		}
	}
	if len(userIDs) == 0 || len(normalized) == 0 {
		return blocked, nil
	}
	var filters []UserTrustedSender
	err := session.Table(new(UserTrustedSender)).Where(builder.And(
		builder.In("user_id", userIDs),
		builder.In("LOWER(email)", normalized),
		builder.Eq{"filter_type": FilterTypeBlacklist},
	)).Find(&filters)
	if err != nil {
		return nil, fmt.Errorf("load recipient sender filters: %w", err)
	}
	for _, filter := range filters {
		blocked[filter.UserID] = true
	}
	return blocked, nil
}
