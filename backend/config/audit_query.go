package config

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrAuditQueryUnavailable = errors.New("configuration audit query unavailable")
	ErrInvalidAuditQuery     = errors.New("configuration audit query is invalid")
	ErrInvalidAuditFormat    = errors.New("configuration audit export format is invalid")
	ErrAuditNotFound         = errors.New("configuration audit event not found")
)

// ValidateConfigAuditQuery 校验审计查询条件，拒绝未知或越界参数。
func ValidateConfigAuditQuery(query ConfigAuditQuery) error {
	if query.ID != "" && !validAuditEventID(query.ID) {
		return ErrInvalidAuditQuery
	}
	if query.Limit < 0 || query.Limit > 1000 {
		return ErrInvalidAuditQuery
	}
	if !query.From.IsZero() && !query.To.IsZero() && query.From.After(query.To) {
		return ErrInvalidAuditQuery
	}
	if len(query.ActorID) > 128 || strings.ContainsAny(query.ActorID, "\r\n") {
		return ErrInvalidAuditQuery
	}
	if query.Operation != "" && query.Operation != ConfigOperationUpdate && query.Operation != ConfigOperationRollback && query.Operation != ConfigOperationMigration {
		return ErrInvalidAuditQuery
	}
	if query.Section != "" && query.Section != SectionRoot && query.Section != SectionSMTP && query.Section != SectionEmail && query.Section != SectionSSL && query.Section != SectionPlugins {
		return ErrInvalidAuditQuery
	}
	return nil
}

// AuditQuery 读取配置审计的只读接口，写入器不支持查询时明确拒绝。
func (m *ConfigManager) AuditQuery(ctx context.Context, query ConfigAuditQuery) ([]ConfigAuditEvent, error) {
	if err := ValidateConfigAuditQuery(query); err != nil {
		return nil, err
	}
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
	if err := ValidateConfigAuditQuery(query); err != nil {
		return nil, err
	}
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
	if err := ValidateConfigAuditQuery(query); err != nil {
		return nil, err
	}
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

// AuditDetail 按稳定标识查询一条配置审计事件。
func (m *ConfigManager) AuditDetail(ctx context.Context, id string) (ConfigAuditEvent, error) {
	id = strings.TrimSpace(id)
	if !validAuditEventID(id) {
		return ConfigAuditEvent{}, ErrInvalidAuditQuery
	}
	items, err := m.AuditQuery(ctx, ConfigAuditQuery{ID: id, Limit: 1})
	if err != nil {
		return ConfigAuditEvent{}, err
	}
	if len(items) == 0 {
		return ConfigAuditEvent{}, ErrAuditNotFound
	}
	return items[0], nil
}
