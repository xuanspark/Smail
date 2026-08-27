package config

import (
	"context"
	"errors"
)

var ErrAuditQueryUnavailable = errors.New("configuration audit query unavailable")

// AuditQuery 读取配置审计的只读接口，写入器不支持查询时明确拒绝。
func (m *ConfigManager) AuditQuery(ctx context.Context, query ConfigAuditQuery) ([]ConfigAuditEvent, error) {
	if m == nil || m.security.Audit == nil {
		return nil, ErrAuditQueryUnavailable
	}
	reader, ok := m.security.Audit.(interface {
		Query(context.Context, ConfigAuditQuery) ([]ConfigAuditEvent, error)
	})
	if !ok {
		return nil, ErrAuditQueryUnavailable
	}
	return reader.Query(ctx, query)
}

// ExportAuditJSON 导出脱敏配置审计；导出内容由审计存储实现生成。
func (m *ConfigManager) ExportAuditJSON(ctx context.Context, query ConfigAuditQuery) ([]byte, error) {
	if m == nil || m.security.Audit == nil {
		return nil, ErrAuditQueryUnavailable
	}
	reader, ok := m.security.Audit.(interface {
		ExportJSON(context.Context, ConfigAuditQuery) ([]byte, error)
	})
	if !ok {
		return nil, ErrAuditQueryUnavailable
	}
	return reader.ExportJSON(ctx, query)
}

// ExportAuditCSV 导出脱敏配置审计；导出内容由审计存储实现生成。
func (m *ConfigManager) ExportAuditCSV(ctx context.Context, query ConfigAuditQuery) ([]byte, error) {
	if m == nil || m.security.Audit == nil {
		return nil, ErrAuditQueryUnavailable
	}
	reader, ok := m.security.Audit.(interface {
		ExportCSV(context.Context, ConfigAuditQuery) ([]byte, error)
	})
	if !ok {
		return nil, ErrAuditQueryUnavailable
	}
	return reader.ExportCSV(ctx, query)
}
