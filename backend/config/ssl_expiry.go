package config

import (
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

// 调用方持有配置写锁；先事务清除过期引用，再由文件维护回收材料。
func (m *ConfigManager) clearExpiredPreviousSSL() error {
	m.mu.RLock()
	current := cloneRuntimeConfig(m.current)
	pending := m.pending
	m.mu.RUnlock()
	if pending != nil {
		if pending.Committed && pending.Actor.UserID == "ssl-maintenance" {
			return m.publishExpiredSSL(pending.Candidate)
		}
		return nil
	}
	ssl := current.SSL.TLS
	if ssl.PreviousCertificateRef == "" {
		return nil
	}
	path, err := resolveCustomSSLInput(m.loader.root, ssl.PreviousCertificateRef)
	if err != nil {
		return err
	}
	if _, err := resolveManagedSSLReferencesAtRoot(m.loader.root, ssl); err != nil {
		return err
	}
	_, statErr := os.Lstat(path)
	if !os.IsNotExist(statErr) {
		data, err := readExternalSSLFile(path, false)
		if err != nil {
			return err
		}
		certificates, err := parseCertificatePEM(data)
		if err != nil || len(certificates) == 0 {
			return ErrInvalidSSLMaterial
		}
		if time.Now().Before(certificates[0].NotAfter) {
			return nil
		}
	}
	candidate := cloneRuntimeConfig(current)
	candidate.SSL.TLS.PreviousCertificateRef = ""
	candidate.SSL.TLS.PreviousPrivateKeyRef = ""
	setCandidateGeneration(&candidate, current.Generation+1)
	if err := m.snapshots.Validate(candidate); err != nil {
		return err
	}
	id, err := newRollbackTicketID()
	if err != nil {
		return err
	}
	if err := m.transactions.Commit(current, candidate); err != nil {
		return err
	}
	// 事务已经落盘时保留恢复入口；下轮维护重试发布，不再次提交同一代次。
	m.mu.Lock()
	m.pending = &pendingConfig{ID: id, Candidate: candidate, Section: SectionSSL, Committed: true,
		Actor: AuditActor{UserID: "ssl-maintenance", Role: "system"}}
	m.mu.Unlock()
	return m.publishExpiredSSL(candidate)
}

func (m *ConfigManager) publishExpiredSSL(candidate RuntimeConfig) error {
	if err := m.publishSnapshot(candidate); err != nil {
		return err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.current = candidate
	m.pending = nil
	m.status.SetupState = candidate.SetupState
	m.status.DegradedReasons = nil
	m.status.ActiveGeneration = candidate.Generation
	m.status.LastValidAt = &now
	m.mu.Unlock()
	log.WithFields(log.Fields{"action": "清理过期证书引用", "result": "success"}).Info("上一套证书已到期，已解除保留")
	return nil
}
