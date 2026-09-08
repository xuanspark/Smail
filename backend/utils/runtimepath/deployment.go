package runtimepath

import (
	"errors"
	"os"
	"path/filepath"
)

// CheckKeyLayout 拒绝同时存在新旧密钥，避免误用另一套加密材料。
func CheckKeyLayout(root string) error {
	legacy, modern := false, false
	for _, name := range []string{"credential.key", "external-email-encryption.key", "external-email-fingerprint.key"} {
		if _, err := os.Lstat(filepath.Join(root, "data", name)); err == nil {
			legacy = true
		} else if !os.IsNotExist(err) {
			return err
		}
		if _, err := os.Lstat(filepath.Join(root, "ssl", name)); err == nil {
			modern = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if legacy && modern {
		return errors.New("新旧目录同时存在密钥，必须先人工确认迁移，不能自动选择")
	}
	return nil
}

// DeploymentLayout 识别已迁移部署，旧部署继续只读原路径，不能静默生成替代密钥。
func DeploymentLayout(root string) bool {
	if _, err := os.Lstat(filepath.Join(root, "data", "credential.key")); err == nil {
		return false
	}
	for _, name := range []string{"data/smail-state.db", "ssl/credential.key"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func SecureDirectory(root, name string) (string, error) {
	directory := filepath.Join(root, name)
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := safeDirectory(directory); err != nil {
		return "", err
	}
	return directory, os.Chmod(directory, 0700)
}

func KeyDirectory(root string) string {
	if DeploymentLayout(root) {
		return filepath.Join(root, "ssl")
	}
	return filepath.Join(root, "data")
}

func SSLReference(root, value string) string {
	value = LegacyReference(value)
	if !DeploymentLayout(root) {
		return value
	}
	for _, prefix := range []string{"data/ssl", "data/dkim"} {
		if value == prefix || len(value) > len(prefix) && value[:len(prefix)+1] == prefix+"/" {
			if prefix == "data/dkim" {
				return "ssl/dkim" + value[len(prefix):]
			}
			return "ssl" + value[len(prefix):]
		}
	}
	return value
}

func SSLDirectory(root string) string {
	return filepath.Join(root, filepath.FromSlash(SSLReference(root, "data/ssl")))
}

func DKIMDirectory(root string) string {
	return filepath.Join(root, filepath.FromSlash(SSLReference(root, "data/dkim")))
}
