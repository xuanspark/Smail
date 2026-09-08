package config

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"time"
)

// Visit 从旧文件流式迁移，不受管理界面的查询条数上限影响，不修改源文件。
func (w *FileAuditWriter) Visit(ctx context.Context, from time.Time, visit func(ConfigAuditEvent) error) error {
	if w == nil || visit == nil {
		return ErrAuditStorageUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := os.Lstat(w.path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return ErrAuditStorageUnavailable
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ErrAuditStorageUnavailable
	}
	file, err := os.Open(w.path)
	if err != nil {
		return ErrAuditStorageUnavailable
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	line := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line++
		var event ConfigAuditEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return ErrAuditStorageUnavailable
		}
		if event.ID == "" {
			event.ID = legacyAuditEventID(scanner.Bytes(), line)
		}
		if err := validateAuditEvent(event); err != nil {
			return err
		}
		if event.CreatedAt.Before(from) {
			continue
		}
		if err := visit(event); err != nil {
			return err
		}
	}
	return scanner.Err()
}
