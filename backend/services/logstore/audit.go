package logstore

import (
	"context"
	"encoding/json"

	"git.pcux.cn/xuans/Smail/config"
)

type AuditWriter struct{ Store *Store }

func (w *AuditWriter) WriteConfigAudit(ctx context.Context, event config.ConfigAuditEvent) error {
	// 旧调用方也转到运行日志，避免再次接入数据库审计。
	return (config.RuntimeConfigLogWriter{}).WriteConfigAudit(ctx, event)
}

func (w *AuditWriter) Query(ctx context.Context, query config.ConfigAuditQuery) ([]config.ConfigAuditEvent, error) {
	if w == nil || w.Store == nil {
		return nil, ErrUnavailable
	}
	if err := config.ValidateConfigAuditQuery(query); err != nil {
		return nil, err
	}
	entries, err := w.Store.Query(ctx, Query{
		Kind: "config", ID: query.ID, From: query.From, To: query.To, ActorID: query.ActorID,
		Action: string(query.Operation), Section: string(query.Section), Limit: query.Limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]config.ConfigAuditEvent, 0, len(entries))
	for _, entry := range entries {
		var event config.ConfigAuditEvent
		if json.Unmarshal(entry.Payload, &event) != nil {
			return nil, ErrUnavailable
		}
		items = append(items, event)
	}
	return items, nil
}

func (w *AuditWriter) ExportJSON(ctx context.Context, query config.ConfigAuditQuery) ([]byte, error) {
	items, err := w.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(items, "", "  ")
}

func (w *AuditWriter) ExportCSV(ctx context.Context, query config.ConfigAuditQuery) ([]byte, error) {
	items, err := w.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return config.ExportConfigAuditCSV(items)
}

func (w *AuditWriter) ImportLegacy(_ context.Context, _ string) error {
	// 独立配置审计已停用，历史文件留在原处，不再导入数据库或重复输出。
	return nil
}
