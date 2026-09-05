package config

import (
	"errors"
	"time"
)

var ErrBackupListingUnavailable = errors.New("configuration backup listing unavailable")

type ConfigBackupInfo struct {
	Generation uint64    `json:"generation"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ListBackups 返回可回滚配置的脱敏清单，不暴露备份路径和文件名。
func (m *ConfigManager) ListBackups() ([]ConfigBackupInfo, error) {
	if m == nil || m.transactions == nil {
		return nil, ErrBackupListingUnavailable
	}
	return m.transactions.ListBackups()
}
