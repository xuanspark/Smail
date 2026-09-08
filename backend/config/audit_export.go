package config

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func exportAuditCSV(events []ConfigAuditEvent) ([]byte, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	if err := writer.Write([]string{"id", "operation", "section", "actor_id", "actor_role", "from_generation", "to_generation", "target_generation", "changed_fields", "confirmation_method", "result", "created_at", "actor_name", "changes"}); err != nil {
		return nil, err
	}
	for _, event := range events {
		changes, err := json.Marshal(event.Changes)
		if err != nil {
			return nil, err
		}
		cells := []string{
			event.ID,
			string(event.Operation),
			string(event.Section),
			event.ActorID,
			event.ActorRole,
			fmt.Sprintf("%d", event.FromGeneration),
			fmt.Sprintf("%d", event.ToGeneration),
			fmt.Sprintf("%d", event.TargetGeneration),
			strings.Join(event.ChangedFields, ","),
			event.ConfirmationMethod,
			event.Result,
			event.CreatedAt.UTC().Format(time.RFC3339Nano),
			event.ActorName,
			string(changes),
		}
		for index, cell := range cells {
			if strings.HasPrefix(cell, "=") || strings.HasPrefix(cell, "+") || strings.HasPrefix(cell, "-") || strings.HasPrefix(cell, "@") || strings.HasPrefix(cell, "\t") || strings.HasPrefix(cell, "\r") {
				cells[index] = "'" + cell
			}
		}
		if err := writer.Write(cells); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// ExportConfigAuditCSV 让文件、内存和数据库导出共享字段及表格公式保护。
func ExportConfigAuditCSV(events []ConfigAuditEvent) ([]byte, error) {
	return exportAuditCSV(events)
}
