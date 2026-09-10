package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"git.pcux.cn/xuans/Smail/services/access"
)

type operationLogActorRole struct {
	ID   string
	Role string
}

// applyOperationLogActorRoleSchema 将操作发生时的角色从历史载荷回填到独立字段。
func applyOperationLogActorRoleSchema(ctx context.Context, exec executor, dialect Dialect) error {
	definitions := map[Dialect]string{
		DialectSQLite:   `TEXT NULL`,
		DialectMySQL:    `VARCHAR(20) NULL`,
		DialectPostgres: `VARCHAR(20) NULL`,
	}
	definition, ok := definitions[dialect]
	if !ok {
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	if err := ensureColumn(ctx, exec, dialect, "operation_log", "actor_role", definition); err != nil {
		return err
	}

	rows, err := exec.QueryContext(ctx, "SELECT id, payload FROM operation_log")
	if err != nil {
		return fmt.Errorf("query operation log history: %w", err)
	}
	items := make([]operationLogActorRole, 0)
	for rows.Next() {
		var item operationLogActorRole
		var payload string
		if err := rows.Scan(&item.ID, &payload); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan operation log history: %w", err)
		}
		item.Role = historicalActorRole(payload)
		if item.Role != "" {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate operation log history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close operation log history: %w", err)
	}

	update := `UPDATE "operation_log" SET "actor_role" = ? WHERE "id" = ? AND "actor_role" IS NULL`
	if dialect == DialectMySQL {
		update = "UPDATE `operation_log` SET `actor_role` = ? WHERE `id` = ? AND `actor_role` IS NULL"
	} else if dialect == DialectPostgres {
		update = `UPDATE "operation_log" SET "actor_role" = $1 WHERE "id" = $2 AND "actor_role" IS NULL`
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, update, item.Role, item.ID); err != nil {
			return fmt.Errorf("backfill operation log actor role: %w", err)
		}
	}
	return nil
}

func historicalActorRole(payload string) string {
	var event struct {
		ActorRole string `json:"actorRole"`
	}
	if json.Unmarshal([]byte(payload), &event) != nil {
		return ""
	}
	role := access.Role(strings.TrimSpace(event.ActorRole))
	if !role.Valid() {
		return ""
	}
	return string(role)
}
