package config

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

const ConfigAuditRetention = 365 * 24 * time.Hour

var (
	ErrAuditStorageUnavailable = errors.New("configuration audit storage unavailable")
	ErrAuditEventInvalid       = errors.New("configuration audit event is invalid")
)

type ConfigAuditQuery struct {
	From      time.Time
	To        time.Time
	ActorID   string
	Operation ConfigOperation
	Section   Section
	Limit     int
}

type FileAuditWriter struct {
	path string
	mu   sync.Mutex
}

func NewFileAuditWriter(root string) (*FileAuditWriter, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrAuditStorageUnavailable
	}
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "config-audit-v2.jsonl")
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, ErrAuditStorageUnavailable
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	return &FileAuditWriter{path: path}, nil
}

func (w *FileAuditWriter) WriteConfigAudit(ctx context.Context, event ConfigAuditEvent) error {
	if err := auditContextError(ctx); err != nil {
		return err
	}
	if w == nil || w.path == "" {
		return ErrAuditStorageUnavailable
	}
	event, err := normalizeAuditEvent(event)
	if err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return ErrAuditStorageUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ensureAuditFile(w.path); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return ErrAuditStorageUnavailable
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return ErrAuditStorageUnavailable
	}
	if err := file.Sync(); err != nil {
		return ErrAuditStorageUnavailable
	}
	return nil
}

func (w *FileAuditWriter) Query(ctx context.Context, query ConfigAuditQuery) ([]ConfigAuditEvent, error) {
	if err := auditContextError(ctx); err != nil {
		return nil, err
	}
	if w == nil || w.path == "" {
		return nil, ErrAuditStorageUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := os.Lstat(w.path); err != nil {
		if os.IsNotExist(err) {
			return []ConfigAuditEvent{}, nil
		}
		return nil, ErrAuditStorageUnavailable
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrAuditStorageUnavailable
	}
	file, err := os.Open(w.path)
	if err != nil {
		return nil, ErrAuditStorageUnavailable
	}
	defer file.Close()
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	result := make([]ConfigAuditEvent, 0, minAuditLimit(limit, 32))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var event ConfigAuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, ErrAuditStorageUnavailable
		}
		if err := validateAuditEvent(event); err != nil {
			return nil, ErrAuditStorageUnavailable
		}
		if !auditEventMatches(event, query) {
			continue
		}
		result = append(result, event)
		if len(result) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, ErrAuditStorageUnavailable
	}
	return result, nil
}

func (w *FileAuditWriter) ExportJSON(ctx context.Context, query ConfigAuditQuery) ([]byte, error) {
	events, err := w.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return json.Marshal(events)
}

func (w *FileAuditWriter) ExportCSV(ctx context.Context, query ConfigAuditQuery) ([]byte, error) {
	events, err := w.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	if err := writer.Write([]string{"operation", "section", "actor_id", "actor_role", "from_generation", "to_generation", "target_generation", "changed_fields", "confirmation_method", "result", "created_at"}); err != nil {
		return nil, err
	}
	for _, event := range events {
		if err := writer.Write([]string{
			string(event.Operation), string(event.Section), event.ActorID, event.ActorRole,
			fmt.Sprintf("%d", event.FromGeneration), fmt.Sprintf("%d", event.ToGeneration),
			fmt.Sprintf("%d", event.TargetGeneration), strings.Join(event.ChangedFields, ","),
			event.ConfirmationMethod, event.Result, event.CreatedAt.UTC().Format(time.RFC3339Nano),
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

func (w *FileAuditWriter) Cleanup(ctx context.Context, now time.Time) (int, error) {
	if err := auditContextError(ctx); err != nil {
		return 0, err
	}
	if w == nil || w.path == "" {
		return 0, ErrAuditStorageUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	file, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, ErrAuditStorageUnavailable
	}
	defer file.Close()
	cutoff := now.Add(-ConfigAuditRetention)
	kept := make([][]byte, 0, 64)
	deleted := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var event ConfigAuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || validateAuditEvent(event) != nil {
			return deleted, ErrAuditStorageUnavailable
		}
		if event.CreatedAt.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, append([]byte(nil), scanner.Bytes()...))
	}
	if err := scanner.Err(); err != nil {
		return deleted, ErrAuditStorageUnavailable
	}
	if err := file.Close(); err != nil {
		return deleted, ErrAuditStorageUnavailable
	}
	if deleted == 0 {
		return 0, nil
	}
	data := make([]byte, 0, len(kept)*128)
	for _, line := range kept {
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := atomicfile.Write(w.path, data, 0600); err != nil {
		return deleted, ErrAuditStorageUnavailable
	}
	return deleted, nil
}

func ensureAuditFile(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrAuditStorageUnavailable
		}
		if err := os.Chmod(path, 0600); err != nil {
			return ErrAuditStorageUnavailable
		}
		return nil
	} else if !os.IsNotExist(err) {
		return ErrAuditStorageUnavailable
	}
	return nil
}

func normalizeAuditEvent(event ConfigAuditEvent) (ConfigAuditEvent, error) {
	if err := validateAuditEvent(event); err != nil {
		return ConfigAuditEvent{}, err
	}
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.ActorRole = strings.TrimSpace(event.ActorRole)
	event.ConfirmationMethod = strings.ToLower(strings.TrimSpace(event.ConfirmationMethod))
	event.ChangedFields = append([]string(nil), event.ChangedFields...)
	return event, nil
}

func validateAuditEvent(event ConfigAuditEvent) error {
	if event.Operation != ConfigOperationUpdate && event.Operation != ConfigOperationRollback && event.Operation != ConfigOperationMigration {
		return ErrAuditEventInvalid
	}
	if strings.TrimSpace(event.ActorID) == "" || strings.TrimSpace(event.ActorRole) == "" || event.FromGeneration == 0 || event.ToGeneration == 0 || event.CreatedAt.IsZero() {
		return ErrAuditEventInvalid
	}
	if event.Result != "success" && event.Result != "failure" {
		return ErrAuditEventInvalid
	}
	if strings.ContainsAny(event.ActorID+event.ActorRole+event.ConfirmationMethod, "\r\n") || len(event.ActorID) > 128 || len(event.ActorRole) > 64 {
		return ErrAuditEventInvalid
	}
	for _, field := range event.ChangedFields {
		if field == "" || len(field) > 64 || strings.ContainsAny(field, "\r\n") {
			return ErrAuditEventInvalid
		}
	}
	return nil
}

func auditEventMatches(event ConfigAuditEvent, query ConfigAuditQuery) bool {
	if !query.From.IsZero() && event.CreatedAt.Before(query.From) {
		return false
	}
	if !query.To.IsZero() && event.CreatedAt.After(query.To) {
		return false
	}
	if query.ActorID != "" && event.ActorID != query.ActorID {
		return false
	}
	if query.Operation != "" && event.Operation != query.Operation {
		return false
	}
	if query.Section != "" && event.Section != query.Section {
		return false
	}
	return true
}

func auditContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func minAuditLimit(left, right int) int {
	if left < right {
		return left
	}
	return right
}
