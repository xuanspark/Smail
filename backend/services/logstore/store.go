// Package logstore 在当前业务数据库中保存操作与收发信日志。
package logstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

var ErrUnavailable = errors.New("日志数据库不可用")
var ErrInvalid = errors.New("日志参数无效")

type Entry struct {
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"createdAt"`
	Kind      string          `json:"kind"`
	ActorID   string          `json:"actorId"`
	Action    string          `json:"action"`
	Section   string          `json:"section"`
	Result    string          `json:"result"`
	Payload   json.RawMessage `json:"payload"`
}

type Query struct {
	// ScopeActorID 不接受客户端直接赋值；非空时由服务端强制限制可见账号。
	ScopeActorID         string
	IncludeOrdinaryUsers bool
	ID                   string
	Kind                 string
	ActorID              string
	Action               string
	Section              string
	From                 time.Time
	To                   time.Time
	Limit                int
	Offset               int
}

type Store struct {
	db      *sql.DB
	dialect migration.Dialect
	now     func() time.Time
}

func New(database *sql.DB, dialect migration.Dialect) (*Store, error) {
	if database == nil {
		return nil, ErrUnavailable
	}
	if _, err := migration.ParseDialect(string(dialect)); err != nil {
		return nil, ErrInvalid
	}
	return &Store{db: database, dialect: dialect, now: time.Now}, nil
}

func logTable(kind string) (string, error) {
	switch kind {
	case "config", "operation":
		return "operation_log", nil
	case "mail":
		return "mail_log", nil
	default:
		return "", ErrInvalid
	}
}

func (s *Store) bind(statement string) string {
	if s.dialect != migration.DialectPostgres {
		return statement
	}
	index := 0
	return replacePlaceholders(statement, &index)
}

func replacePlaceholders(statement string, index *int) string {
	parts := strings.Split(statement, "?")
	var builder strings.Builder
	for position, part := range parts {
		if position > 0 {
			*index++
			fmt.Fprintf(&builder, "$%d", *index)
		}
		builder.WriteString(part)
	}
	return builder.String()
}

// Write 只接受业务点构造的脱敏载荷，禁止直接传入 HTTP 请求或邮件正文。
func (s *Store) Write(ctx context.Context, entry Entry) error {
	if entry.Kind == "config" {
		return ErrInvalid
	}
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	table, err := logTable(entry.Kind)
	if err != nil {
		return err
	}
	if entry.CreatedAt.IsZero() || len(entry.ID) > 80 || len(entry.ActorID) > 128 || len(entry.Action) > 64 || len(entry.Section) > 32 || len(entry.Result) > 16 || len(entry.Payload) > 256*1024 || !json.Valid(entry.Payload) {
		return ErrInvalid
	}
	// 迁移、重放都不能让过期记录重新进入保留窗口。
	if entry.CreatedAt.Before(logpolicy.Cutoff(s.now())) {
		return nil
	}
	if emptyEntry(entry) {
		return nil
	}
	if entry.ID == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return ErrUnavailable
		}
		entry.ID = "log-" + hex.EncodeToString(token[:])
	}
	statement := "INSERT INTO " + table + " (id, created_at, kind, actor_id, action, section, result, payload) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	// 仅忽略同一稳定标识的重放，不用 INSERT IGNORE 掩盖截断、缺字段等错误。
	if s.dialect == migration.DialectMySQL {
		statement += " ON DUPLICATE KEY UPDATE id = id"
	} else {
		statement += " ON CONFLICT (id) DO NOTHING"
	}
	_, err = s.db.ExecContext(ctx, s.bind(statement), entry.ID, entry.CreatedAt.UTC().UnixNano(), entry.Kind, entry.ActorID, entry.Action, entry.Section, entry.Result, string(entry.Payload))
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) Query(ctx context.Context, query Query) ([]Entry, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	table, err := logTable(query.Kind)
	if err != nil || query.Limit < 0 || query.Limit > 1000 || query.Offset < 0 || query.Offset > 100000 || (!query.From.IsZero() && !query.To.IsZero() && query.From.After(query.To)) {
		return nil, ErrInvalid
	}
	from := logpolicy.Cutoff(s.now())
	if query.From.After(from) {
		from = query.From
	}
	statement := "SELECT id, created_at, kind, actor_id, action, section, result, payload FROM " + table + " WHERE created_at >= ? AND kind = ?"
	args := []any{from.UnixNano(), query.Kind}
	if query.ScopeActorID != "" {
		statement += " AND (actor_id = ?"
		args = append(args, query.ScopeActorID)
		if query.IncludeOrdinaryUsers {
			statement += " OR ("
			userTable, idType := `"user"`, "TEXT"
			if s.dialect == migration.DialectMySQL {
				userTable, idType = "`user`", "CHAR"
			}
			statement += "actor_id IN (SELECT CAST(id AS " + idType + ") FROM " + userTable + " WHERE " + models.OrdinaryUserPredicate + ")"
			// 代管视图还要核对操作发生时的角色，不能因账号降级暴露旧管理操作。
			// 缺少历史角色的操作仅本人和超级管理员可见，收发信元数据不包含管理操作。
			if query.Kind == "operation" {
				switch s.dialect {
				case migration.DialectMySQL:
					statement += " AND CASE WHEN JSON_VALID(payload) THEN JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actorRole')) ELSE NULL END = 'user'"
				case migration.DialectPostgres:
					statement += " AND (payload::jsonb ->> 'actorRole') = 'user'"
				default:
					statement += " AND CASE WHEN json_valid(payload) THEN json_extract(payload, '$.actorRole') ELSE NULL END = 'user'"
				}
			}
			statement += ")"
		}
		statement += ")"
	}
	if !query.To.IsZero() {
		statement += " AND created_at <= ?"
		args = append(args, query.To.UnixNano())
	}
	for _, filter := range []struct{ column, value string }{
		{"id", query.ID}, {"actor_id", query.ActorID}, {"action", query.Action}, {"section", query.Section},
	} {
		if filter.value != "" {
			statement += " AND " + filter.column + " = ?"
			args = append(args, filter.value)
		}
	}
	limit := query.Limit
	if limit == 0 {
		limit = 100
	}
	statement += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, query.Offset)
	rows, err := s.db.QueryContext(ctx, s.bind(statement), args...)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	items := make([]Entry, 0)
	for rows.Next() {
		var entry Entry
		var timestamp int64
		var payload string
		if err := rows.Scan(&entry.ID, &timestamp, &entry.Kind, &entry.ActorID, &entry.Action, &entry.Section, &entry.Result, &payload); err != nil {
			return nil, ErrUnavailable
		}
		entry.CreatedAt = time.Unix(0, timestamp).UTC()
		entry.Payload = json.RawMessage(payload)
		items = append(items, entry)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return items, nil
}

// Cleanup 每批先按索引选取标识再删除，避免 MySQL 专属 DELETE LIMIT 和大事务锁表。
func (s *Store) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	if now.IsZero() {
		return 0, ErrInvalid
	}
	cutoff := logpolicy.Cutoff(now).UnixNano()
	var deleted int64
	for _, table := range []string{"operation_log", "mail_log"} {
		for {
			if err := ctx.Err(); err != nil {
				return deleted, err
			}
			rows, err := s.db.QueryContext(ctx, s.bind("SELECT id FROM "+table+" WHERE created_at < ? ORDER BY created_at, id LIMIT ?"), cutoff, logpolicy.DeleteBatchSize)
			if err != nil {
				return deleted, ErrUnavailable
			}
			ids := make([]any, 0, logpolicy.DeleteBatchSize)
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					break
				}
				ids = append(ids, id)
			}
			rowErr := rows.Err()
			closeErr := rows.Close()
			if err != nil || rowErr != nil || closeErr != nil {
				return deleted, ErrUnavailable
			}
			if len(ids) == 0 {
				break
			}
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
			args := append(ids, cutoff)
			result, err := s.db.ExecContext(ctx, s.bind("DELETE FROM "+table+" WHERE id IN ("+placeholders+") AND created_at < ?"), args...)
			if err != nil {
				return deleted, ErrUnavailable
			}
			count, err := result.RowsAffected()
			if err != nil {
				return deleted, ErrUnavailable
			}
			deleted += count
		}
	}
	return deleted, nil
}
