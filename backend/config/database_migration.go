package config

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrDatabaseMigrationTarget = errors.New("database migration target is invalid")

// SwitchDatabase 提交已完成复制与校验的目标连接，保留旧库并标记重启。
// 普通配置编辑不能绕过本迁移入口切换数据库类型。
func (m *ConfigManager) SwitchDatabase(ctx context.Context, expectedGeneration uint64, target DatabaseConfig, actor AuditActor) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if m == nil || expectedGeneration == 0 {
		return ErrManagerStorage
	}
	if actor.UserID == "" || actor.SessionID == "" {
		return ErrAuthorizationRequired
	}
	if err := m.authorize(ctx, actor, ConfigOperationMigration, SectionRoot); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}

	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	pending := m.pending != nil
	m.mu.RUnlock()
	if pending || expectedGeneration != current.Generation {
		return ErrGenerationConflict
	}

	target = NormalizeDatabaseConfig(target)
	if err := ValidateDatabaseConfig(target); err != nil {
		return ErrDatabaseMigrationTarget
	}
	if SameDatabaseLocation(current.Root.Database, target) {
		return ErrDatabaseMigrationTarget
	}

	candidate := cloneRuntimeConfig(current)
	candidate.Root.Database = target
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := ValidateRuntimeConfig(candidate, m.options); err != nil {
		return err
	}
	if err := ValidateRuntimeSecrets(m.vault, candidate); err != nil {
		return err
	}
	if err := m.snapshots.Validate(candidate); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if _, _, err := m.transactions.CreateBackupIfDue(current, time.Now().UTC()); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := m.transactions.Commit(current, candidate); err != nil {
		return err
	}
	if err := m.publishSnapshot(candidate); err != nil {
		return err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.current = cloneRuntimeConfig(candidate)
	m.pending = nil
	m.status = ConfigStatus{
		SetupState:       candidate.SetupState,
		ActiveGeneration: candidate.Generation,
		LastValidAt:      &now,
		PendingRestart:   []ServiceName{ServiceDatabase},
	}
	m.mu.Unlock()
	changedField := "root.database.name"
	if target.Type == DatabaseTypeSQLite {
		changedField = "root.database.localRef"
	}
	if !sameDatabaseConnection(current.Root.Database, target) {
		changedField = "root.database"
	}
	return m.writeAudit(ctx, ConfigAuditEvent{
		Operation:        ConfigOperationMigration,
		Section:          SectionRoot,
		ActorID:          actor.UserID,
		ActorRole:        actor.Role,
		FromGeneration:   current.Generation,
		ToGeneration:     candidate.Generation,
		TargetGeneration: candidate.Generation,
		ChangedFields:    []string{changedField},
		Result:           "success",
		CreatedAt:        now,
	})
}

// SameDatabaseLocation 不把用户名或密码不同误认为不同数据库。
func SameDatabaseLocation(current, target DatabaseConfig) bool {
	current = NormalizeDatabaseConfig(current)
	target = NormalizeDatabaseConfig(target)
	if current.Type != target.Type {
		return false
	}
	if current.Type == DatabaseTypeSQLite {
		return strings.EqualFold(current.LocalRef, target.LocalRef)
	}
	return strings.EqualFold(current.Host, target.Host) && current.Port == target.Port &&
		strings.EqualFold(current.Name, target.Name)
}

func sameDatabaseConnection(current, target DatabaseConfig) bool {
	if current.Type != target.Type ||
		current.Host != target.Host ||
		current.Port != target.Port ||
		current.Username != target.Username ||
		current.Password != target.Password {
		return false
	}
	if current.Type == DatabaseTypeSQLite {
		return true
	}
	return current.LocalRef == target.LocalRef
}
