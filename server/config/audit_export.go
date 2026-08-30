package config

import (
	"encoding/csv"
	"fmt"
	"strings"
	"time"
)

func exportAuditCSV(events []ConfigAuditEvent) ([]byte, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	if err := writer.Write([]string{"id", "operation", "section", "actor_id", "actor_role", "from_generation", "to_generation", "target_generation", "changed_fields", "confirmation_method", "result", "created_at"}); err != nil {
		return nil, err
	}
	for _, event := range events {
		if err := writer.Write([]string{
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
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}
