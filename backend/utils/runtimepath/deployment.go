package runtimepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CheckKeyLayout 拒绝同时存在新旧密钥，避免误用另一套加密材料。
func CheckKeyLayout(root string) error {
	legacy, modern := false, false
	for _, name := range BusinessKeyNames() {
		if _, err := os.Lstat(filepath.Join(root, "data", name)); err == nil {
			legacy = true
		} else if !os.IsNotExist(err) {
			return err
		}
		if _, err := os.Lstat(groupedKeyPath(root, name)); err == nil {
			modern = true
			if _, err := os.Lstat(filepath.Join(root, "ssl", name)); !os.IsNotExist(err) {
				return errors.New("密钥同时存在于归档目录和旧目录，拒绝自动选择或覆盖")
			}
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
	for _, name := range []string{"data/.config-state.lock", "data/smail-state.db", "ssl/credential.key", "ssl/credentials/credential.key"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func SecureDirectory(root, name string) (string, error) {
	if !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) || name == "." {
		return "", errors.New("受保护目录名称无效")
	}
	if err := safeDirectory(root); err != nil {
		return "", err
	}
	directory := filepath.Join(root, name)
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := safeDirectory(directory); err != nil {
		return "", err
	}
	return directory, os.Chmod(directory, 0700) // #nosec G302
}

func KeyDirectory(root string) string {
	return filepath.Dir(KeyPath(root, "credential.key"))
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
