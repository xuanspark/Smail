package config

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	log "github.com/sirupsen/logrus"
)

// 清理与写入共用互斥锁；暂存材料从创建到提交或取消期间始终受到保护。
var managedSSLStorage = struct {
	sync.Mutex
	staged map[string]func()
}{staged: make(map[string]func())}

type retainedSSLVersion struct {
	directory string
	modified  time.Time
}

func releaseManagedSSL(root string, ssl SSLConfig) {
	managedSSLStorage.Lock()
	defer managedSSLStorage.Unlock()
	releaseManagedSSLLocked(root, ssl)
}

func releaseManagedSSLLocked(root string, ssl SSLConfig) {
	if ssl.CertificateRef == "" {
		return
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(runtimepath.SSLReference(root, ssl.CertificateRef))))
	if err == nil {
		if release := managedSSLStorage.staged[path]; release != nil {
			release()
			delete(managedSSLStorage.staged, path)
		}
		directory := filepath.Dir(path)
		if release := managedSSLStorage.staged[directory]; release != nil {
			release()
			delete(managedSSLStorage.staged, directory)
		}
	}
}

// ReleaseStagedSSL 仅解除暂存保护，不立即删除；失败操作的残留由下一轮维护处理。
func ReleaseStagedSSL(reference ManagedSSLReference) {
	releaseManagedSSL(ROOT_PATH, SSLConfig{CertificateRef: reference.CertificateRef})
}

// 管理器停止或被替换时解除遗留租约；不删除材料，已提交引用仍由磁盘配置保护。
func (m *ConfigManager) releasePendingSSL() {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()
	if pending != nil && m.loader != nil {
		releaseManagedSSL(m.loader.root, pending.Candidate.SSL.TLS)
	}
}

// maintainManagedSSL 必须在配置写锁内调用；失败只告警，不能把已保存配置误报为未保存。
func maintainManagedSSL(root string, active SSLConfig, previous ...SSLConfig) {
	managedSSLStorage.Lock()
	defer managedSSLStorage.Unlock()
	// 与其他进程的配置事务互斥；锁定后再读取生效引用及恢复状态。
	if runtimepath.DeploymentLayout(root) {
		unlock, err := NewTransactionStore(root, RuntimeValidationOptions{}).lockState()
		if err != nil {
			log.Warn("配置事务正在运行，证书历史清理将在下次维护时重试")
			return
		}
		defer unlock()
	}
	releaseManagedSSLLocked(root, active)
	if err := pruneManagedSSLLocked(root, append([]SSLConfig{active}, previous...)); err != nil {
		log.Warn("证书历史自动清理未完成，已保留现场，将在下次维护时重试")
	}
}

// MaintainManagedSSLHistory 用于启动完成后及定时维护；全新安装无证书时不做删除。
func MaintainManagedSSLHistory() {
	if manager := CurrentManager(); manager != nil {
		manager.writeMu.Lock()
		defer manager.writeMu.Unlock()
		if err := manager.clearExpiredPreviousSSL(); err != nil {
			log.WithFields(log.Fields{"action": "清理过期证书引用", "result": "failure"}).Warn("配置维护未完成，下次自动重试")
			return
		}
		manager.mu.RLock()
		active := manager.current.SSL.TLS
		var pending []SSLConfig
		if manager.pending != nil {
			pending = append(pending, manager.pending.Candidate.SSL.TLS)
		}
		manager.mu.RUnlock()
		maintainManagedSSL(manager.loader.root, active, pending...)
		return
	}
	sslCommitMu.Lock()
	defer sslCommitMu.Unlock()
	if cfg := CurrentConfig(); cfg != nil {
		maintainManagedSSL(ROOT_PATH, cfg.SSL)
	}
}

func pruneManagedSSLLocked(root string, protected []SSLConfig) error {
	if len(protected) == 0 || protected[0].CertificateRef == "" {
		return nil
	}
	if !legacyManagedSSLReference(protected[0].CertificateRef) {
		if _, err := resolveManagedSSLReferencesAtRoot(root, protected[0]); err != nil {
			return err
		}
	}
	// 恢复事务可能仍需要旧材料；恢复未完成时不能清理任何版本。
	if entries, err := os.ReadDir(filepath.Join(root, "data", "transaction")); err != nil && !os.IsNotExist(err) || len(entries) > 0 {
		return ErrConfigRecoveryRequired
	}
	if _, err := os.Lstat(filepath.Join(root, "data", "smail-state.db")); err == nil {
		store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
		store.legacyState = true
		records, err := store.stateRecords("transaction")
		if err != nil || len(records) != 0 {
			return errors.Join(ErrConfigRecoveryRequired, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	keep := make(map[string]bool)
	for _, ssl := range protected {
		if ssl.CertificateRef == "" {
			continue
		}
		reference, err := resolveManagedSSLPathsAtRoot(root, ssl)
		if err != nil {
			return err
		}
		if legacyManagedSSLReference(ssl.CertificateRef) {
			keep[filepath.Dir(reference.CertificatePath)] = true
		}
	}
	// 磁盘引用也必须保留，避免其他管理器实例或已提交未发布的候选被删除。
	configRoot, err := os.OpenRoot(filepath.Join(root, "config"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := configRoot.ReadFile("ssl.json")
	_ = configRoot.Close()
	if err == nil {
		var section SSLSectionConfig
		if err := ParseStrict(data, &section); err != nil {
			return err
		}
		if section.TLS.CertificateRef != "" {
			protected = append(protected, section.TLS)
			reference, err := resolveManagedSSLPathsAtRoot(root, section.TLS)
			if err != nil {
				return err
			}
			if legacyManagedSSLReference(section.TLS.CertificateRef) {
				keep[filepath.Dir(reference.CertificatePath)] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	var versions []retainedSSLVersion
	// 持有租约直到整轮清理结束，避免检查解锁后与另一个进程的导入操作竞态。
	var leases []func()
	defer func() {
		for _, release := range leases {
			release()
		}
	}()
	for _, mode := range []string{ManagedSSLModeDefault, ManagedSSLModeUpload} {
		directory, err := managedSSLRootPathAtRoot(root, mode)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil || filepath.Clean(resolved) != filepath.Clean(directory) {
			return ErrUnsafeSSLFile
		}
		for _, entry := range entries {
			name := entry.Name()
			// 只处理程序生成的完整版本；未知目录、候选和其他用户文件一律不删。
			known := name == "current" || validSSLVersion(name) || strings.HasPrefix(name, ".previous-") && validSSLVersion("version-"+strings.TrimPrefix(name, ".previous-"))
			if !known {
				continue
			}
			path := filepath.Join(directory, name)
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrUnsafeSSLFile
			}
			if managedSSLStorage.staged[path] != nil {
				keep[path] = true
			}
			if keep[path] {
				versions = append(versions, retainedSSLVersion{directory: path, modified: info.ModTime()})
				continue
			}
			// 其他进程正在导入的版本持有私钥文件的独占租约，不依赖本进程内存判断。
			lease, err := openSSLLease(filepath.Join(path, "private.key"))
			if err != nil {
				keep[path] = true
			} else {
				unlock, err := lockStateFile(lease)
				if err != nil {
					keep[path] = true
					if closeErr := lease.Close(); closeErr != nil {
						return errors.Join(err, closeErr)
					}
				} else {
					leases = append(leases, func() { releaseConfigLease(unlock, lease) })
				}
			}
			versions = append(versions, retainedSSLVersion{directory: path, modified: info.ModTime()})
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].modified.Equal(versions[j].modified) {
			return versions[i].directory > versions[j].directory
		}
		return versions[i].modified.After(versions[j].modified)
	})
	// 尚未迁移的旧配置暂时沿用旧规则；迁移完成后不再为 managed 额外保留副本。
	if legacyManagedSSLReference(protected[0].CertificateRef) {
		for _, version := range versions {
			if len(keep) >= 2 {
				break
			}
			keep[version.directory] = true
		}
	}
	var result error
	for _, version := range versions {
		if !keep[version.directory] {
			result = errors.Join(result, removeManagedSSLDirectory(version.directory))
		}
	}
	if result == nil {
		// 仅删除已空的旧目录；未知文件会使 Remove 失败并原样保留。
		for _, mode := range []string{ManagedSSLModeDefault, ManagedSSLModeUpload} {
			directory, err := managedSSLRootPathAtRoot(root, mode)
			if err == nil {
				_ = os.Remove(directory)
				_ = os.Remove(filepath.Dir(directory))
			}
		}
	}
	return errors.Join(result, pruneFlatSSLLocked(root, protected))
}

// 回滚前必须检查实际证书，不能提交已经被保留策略淘汰的历史引用。
func validateRollbackSSL(root string, candidate RuntimeConfig) error {
	ssl := candidate.SSL.TLS
	if ssl.CertificateRef == "" && ssl.PrivateKeyRef == "" {
		return nil
	}
	if _, err := resolveManagedSSLReferencesAtRoot(root, ssl); err != nil {
		return ErrSSLHistoryUnavailable
	}
	return nil
}

var ErrSSLHistoryUnavailable = errors.New("历史证书已清理或不可用，无法回滚此配置，请重新导入有效证书")
