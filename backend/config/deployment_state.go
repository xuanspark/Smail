package config

import (
	"errors"
	"os"
	"path/filepath"

	fileutil "git.pcux.cn/xuans/Smail/utils/file"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
)

// InitializeDeployment 为全新部署创建状态库，并自动归档已采用 ssl 布局的原有业务密钥。
// 更旧的 config/data 布局仍需完整迁移，不能只搬密钥而切断证书或数据库引用。
func InitializeDeployment(root string) error {
	if runtimepath.DeploymentLayout(root) {
		return initializeDeploymentKeys(root)
	}
	for _, name := range []string{"config/config.json", "data/credential.key", "config/credential.key"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	db, err := NewTransactionStore(root, RuntimeValidationOptions{}).openState()
	if err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return initializeDeploymentKeys(root)
}

func initializeDeploymentKeys(root string) error {
	unlock, err := NewTransactionStore(root, RuntimeValidationOptions{}).lockState()
	if err != nil {
		return err
	}
	defer unlock()
	return runtimepath.PrepareDeploymentKeys(root)
}

// ImportDeploymentHistory 在停服迁移期间导入旧历史原始字节，原目录保持不动。
func ImportDeploymentHistory(source, target string, validation RuntimeValidationOptions) error {
	if !runtimepath.DeploymentLayout(target) {
		return errors.New("目标部署密钥尚未迁移到 ssl")
	}
	if entries, err := os.ReadDir(filepath.Join(source, "data", "transaction")); err == nil && len(entries) != 0 {
		return ErrConfigRecoveryRequired
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	store := NewTransactionStore(target, validation)
	unlock, err := store.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(filepath.Join(source, "data", "backups-v2"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		directory := filepath.Join(source, "data", "backups-v2", entry.Name())
		_, manifest, err := loadConfigBackup(directory, validation)
		if err != nil {
			return err
		}
		files := make(map[string][]byte)
		for _, name := range transactionFileNames() {
			files[name], err = fileutil.ReadRegularFile(filepath.Join(directory, "files", name), maxConfigFileSize)
			if err != nil {
				return err
			}
		}
		if err := validateStateFiles(files, manifest.Files); err != nil {
			return err
		}
		if err := store.saveState("backup", manifest.ID, stateRecord{Backup: &manifest, Original: files}); err != nil {
			return err
		}
	}
	_, err = store.stateBackups()
	return err
}

// NormalizeDeploymentReferences 通过正常事务更新路径及摘要，不能直接编辑配置 JSON。
func NormalizeDeploymentReferences(root string, validation RuntimeValidationOptions) error {
	loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: validation})
	current, err := loader.Load()
	if err != nil {
		return err
	}
	if !runtimepath.DeploymentLayout(root) {
		return ErrManagerStorage
	}
	candidate := cloneRuntimeConfig(current)
	ssl := &candidate.SSL.TLS
	ssl.DefaultDirectory = runtimepath.SSLReference(root, ssl.DefaultDirectory)
	ssl.CustomDirectory = runtimepath.SSLReference(root, ssl.CustomDirectory)
	ssl.CertificateRef = runtimepath.SSLReference(root, ssl.CertificateRef)
	ssl.PrivateKeyRef = runtimepath.SSLReference(root, ssl.PrivateKeyRef)
	if candidate.SSL.TLS.CertificateRef == current.SSL.TLS.CertificateRef && candidate.SSL.TLS.PrivateKeyRef == current.SSL.TLS.PrivateKeyRef && candidate.SSL.TLS.DefaultDirectory == current.SSL.TLS.DefaultDirectory && candidate.SSL.TLS.CustomDirectory == current.SSL.TLS.CustomDirectory {
		return nil
	}
	setCandidateGeneration(&candidate, current.Generation+1)
	return NewTransactionStore(root, validation).Commit(current, candidate)
}
