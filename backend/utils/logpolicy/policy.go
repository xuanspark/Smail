// Package logpolicy 统一三类日志的滚动保留期限。
package logpolicy

import "time"

const (
	Retention           = 30 * 24 * time.Hour
	CompressionAge      = 7 * 24 * time.Hour
	MaintenanceInterval = time.Hour
	DeleteBatchSize     = 1000
)

// Cutoff 使用绝对时长，避免月份长度、时区或夏令时改变保留期。
func Cutoff(now time.Time) time.Time {
	return now.UTC().Add(-Retention)
}
