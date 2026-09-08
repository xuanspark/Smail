package logstore

import (
	"context"
	"encoding/json"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

type AuditWriter struct{ Store *Store }

func (w *AuditWriter) WriteConfigAudit(ctx context.Context, event config.ConfigAuditEvent) error {
	if w == nil || w.Store == nil {
		return ErrUnavailable
	}
	normalized, err := config.NormalizeConfigAuditEvent(event)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	return w.Store.Write(ctx, Entry{
		ID: normalized.ID, CreatedAt: normalized.CreatedAt, Kind: "config",
		ActorID: normalized.ActorID, Action: string(normalized.Operation),
		Section: string(normalized.Section), Result: normalized.Result, Payload: payload,
	})
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

func (w *AuditWriter) ImportLegacy(ctx context.Context, root string) error {
	if w == nil || w.Store == nil {
		return ErrUnavailable
	}
	legacy, err := config.NewFileAuditWriter(root)
	if err != nil {
		return err
	}
	return legacy.Visit(ctx, logpolicy.Cutoff(w.Store.now()), func(event config.ConfigAuditEvent) error {
		return w.WriteConfigAudit(ctx, event)
	})
}
