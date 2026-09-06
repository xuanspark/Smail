package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

var sslCommitMu sync.Mutex

// WriteSSLConfig 是初始化及自动续期的内部入口，只允许更新 SSL 引用和 HTTPS 开关。
// 旧证书保持不动；已有人工候选或签发期间域名、证书策略变化时拒绝提交。
func WriteSSLConfig(expected, next *Config) error {
	if expected == nil || next == nil {
		return ErrInvalidSSLReference
	}
	sslCommitMu.Lock()
	defer sslCommitMu.Unlock()
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	data, err := readConfigFile("config.json")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	partitioned := false
	if len(data) > 0 {
		partitioned, err = isPartitionedConfig(data)
		if err != nil {
			return err
		}
	}
	if !partitioned {
		merged := *next
		if len(data) > 0 {
			live, err := readConfig()
			if err != nil {
				return err
			}
			if !sameSSLPolicy(live, expected) {
				return ErrGenerationConflict
			}
			// 签发期间其他设置可能已经保存，只合并本入口负责的 SSL 字段。
			merged = *live
			merged.SSL = next.SSL
			merged.SSLType = next.SSLType
			merged.SSLPrivateKeyPath = next.SSLPrivateKeyPath
			merged.SSLPublicKeyPath = next.SSLPublicKeyPath
			merged.HttpsEnabled = next.HttpsEnabled
		}
		if err := writeConfig(&merged); err != nil {
			return err
		}
		*next = merged
		return nil
	}
	manager := CurrentManager()
	if manager == nil {
		manager, err = NewConfigManager(ROOT_PATH, ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: !DevelopmentMode}})
		if err != nil {
			return err
		}
		SetRuntimeManager(manager)
	}
	if filepath.Clean(manager.loader.root) != filepath.Clean(ROOT_PATH) {
		return ErrGenerationConflict
	}
	manager.writeMu.Lock()
	defer manager.writeMu.Unlock()
	manager.mu.RLock()
	current := cloneRuntimeConfig(manager.current)
	pending := manager.pending != nil
	manager.mu.RUnlock()
	if pending {
		return ErrGenerationConflict
	}
	live, err := readConfig()
	if err != nil {
		return err
	}
	if !sameSSLPolicy(live, expected) {
		return ErrGenerationConflict
	}
	candidate := cloneRuntimeConfig(current)
	candidate.SSL.TLS = next.SSL
	candidate.Root.HTTP.HTTPSEnabled = next.HttpsEnabled == 1
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := ValidateRuntimeConfig(candidate, manager.options); err != nil {
		return err
	}
	if err := ValidateRuntimeSecrets(manager.vault, candidate); err != nil {
		return err
	}
	if err := manager.snapshots.Validate(candidate); err != nil {
		return err
	}
	committed, err := runtimeCompatibility(candidate)
	if err != nil {
		return err
	}
	id, err := newRollbackTicketID()
	if err != nil {
		return err
	}
	if _, _, err := manager.transactions.CreateBackupIfDue(current, time.Now().UTC()); err != nil {
		return err
	}
	if err := manager.transactions.Commit(current, candidate); err != nil {
		return err
	}
	// 已落盘候选必须留在管理器中；运行态发布失败后管理员可显式恢复，而不能取消。
	manager.mu.Lock()
	manager.pending = &pendingConfig{
		ID: id, Candidate: candidate, Section: SectionSSL, Committed: true,
		Actor:           AuditActor{UserID: "system", Role: "system"},
		PreviousRestart: append([]ServiceName(nil), manager.status.PendingRestart...),
	}
	manager.status.PendingRestart = mergeServices(manager.status.PendingRestart, servicesForSection(SectionSSL))
	manager.mu.Unlock()
	if err := manager.publishSnapshot(candidate); err != nil {
		return err
	}
	now := time.Now().UTC()
	manager.mu.Lock()
	manager.current = cloneRuntimeConfig(candidate)
	manager.pending = nil
	manager.status.SetupState = candidate.SetupState
	manager.status.DegradedReasons = nil
	manager.status.ActiveGeneration = candidate.Generation
	manager.status.LastValidAt = &now
	manager.mu.Unlock()
	// 兼容快照与本次已提交候选一致，不再静默忽略提交后的磁盘读取失败。
	*next = *committed
	PublishConfigSnapshot(committed)
	return manager.writeAudit(context.Background(), ConfigAuditEvent{
		Operation: ConfigOperationUpdate, Section: SectionSSL, ActorID: "system", ActorRole: "system",
		FromGeneration: current.Generation, ToGeneration: candidate.Generation,
		ChangedFields: []string{"tls"}, Result: "success", CreatedAt: now,
	})
}

func sameSSLPolicy(left, right *Config) bool {
	return reflect.DeepEqual(left.SSL, right.SSL) && left.SSLType == right.SSLType &&
		left.SSLPrivateKeyPath == right.SSLPrivateKeyPath && left.SSLPublicKeyPath == right.SSLPublicKeyPath &&
		left.Domain == right.Domain && left.WebDomain == right.WebDomain &&
		reflect.DeepEqual(left.Domains, right.Domains) && left.HttpsEnabled == right.HttpsEnabled &&
		left.TLSMode == right.TLSMode
}

func mergeServices(previous, next []ServiceName) []ServiceName {
	result := append([]ServiceName(nil), previous...)
	for _, service := range next {
		found := false
		for _, existing := range result {
			found = found || service == existing
		}
		if !found {
			result = append(result, service)
		}
	}
	return result
}
