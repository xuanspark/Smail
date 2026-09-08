package runtimepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

// BusinessKeyNames 返回固定业务密钥清单，不能由外部输入扩展迁移范围。
func BusinessKeyNames() []string {
	return []string{"credential.key", "external-email-encryption.key", "external-email-fingerprint.key"}
}

func businessKeyFolder(name string) string {
	switch name {
	case "credential.key":
		return "credentials"
	case "external-email-encryption.key":
		return "external-email-encryption"
	case "external-email-fingerprint.key":
		return "external-email-fingerprint"
	default:
		panic("unknown business key")
	}
}

func groupedKeyPath(root, name string) string {
	return filepath.Join(root, "ssl", businessKeyFolder(name), name)
}

// KeyPath 兼容尚未升级的部署；正式启动完成归档后只使用业务子目录。
func KeyPath(root, name string) string {
	folder := businessKeyFolder(name)
	if !DeploymentLayout(root) {
		return filepath.Join(root, "data", name)
	}
	if _, err := os.Lstat(filepath.Join(root, "ssl", name)); err == nil {
		return filepath.Join(root, "ssl", name)
	}
	if _, err := os.Lstat(filepath.Join(root, "ssl", folder)); !os.IsNotExist(err) {
		return groupedKeyPath(root, name)
	}
	return filepath.Join(root, "ssl", name)
}

// SecureKeyDirectory 逐层验证目录，不能通过父目录链接写入外部位置。
func SecureKeyDirectory(root, name string) (string, error) {
	if err := CheckKeyLayout(root); err != nil {
		return "", err
	}
	directory := filepath.Dir(KeyPath(root, name))
	base := "data"
	if DeploymentLayout(root) {
		base = "ssl"
	}
	parent, err := SecureDirectory(root, base)
	if err != nil {
		return "", err
	}
	if filepath.Clean(directory) == filepath.Clean(parent) {
		return parent, nil
	}
	return SecureDirectory(parent, businessKeyFolder(name))
}

// PrepareDeploymentKeys 由主程序启动入口在独占状态锁内调用，不依赖安装脚本。
// 每次重命名保留原字节；中断后的混合位置可在下次启动继续迁移。
func PrepareDeploymentKeys(root string) error {
	if !DeploymentLayout(root) {
		return nil
	}
	if err := CheckReady(root); err != nil {
		return err
	}
	var moves []Move
	for _, name := range BusinessKeyNames() {
		source := KeyPath(root, name)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() != 32 {
			return fmt.Errorf("业务密钥无效，拒绝迁移：%s", name)
		}
		if err := safeDirectory(filepath.Join(root, "ssl")); err != nil {
			return err
		}
		if err := safeDirectory(filepath.Dir(source)); err != nil {
			return err
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return errors.New("业务密钥权限过于宽松，拒绝迁移")
		}
		if source != groupedKeyPath(root, name) {
			moves = append(moves, Move{Source: source, Target: groupedKeyPath(root, name)})
		}
	}
	ssl, err := SecureDirectory(root, "ssl")
	if err != nil {
		return err
	}
	// 先检查所有目标，不能移动第一把密钥后才发现另一业务目录不安全。
	for _, folder := range []string{"credentials", "external-email-encryption", "external-email-fingerprint", "dkim", "managed"} {
		if err := safeDirectory(filepath.Join(ssl, folder)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, folder := range []string{"config", "log"} {
		if _, err := SecureDirectory(root, folder); err != nil {
			return err
		}
	}
	for _, folder := range []string{"credentials", "external-email-encryption", "external-email-fingerprint", "dkim", "managed"} {
		if _, err := SecureDirectory(ssl, folder); err != nil {
			return err
		}
	}
	for _, move := range moves {
		if _, err := os.Lstat(move.Target); !os.IsNotExist(err) {
			return errors.New("密钥迁移目标已存在，拒绝覆盖")
		}
		if err := os.Rename(move.Source, move.Target); err != nil {
			return fmt.Errorf("业务密钥归档未完成，下次启动将继续：%w", err)
		}
		if err := atomicfile.SyncDirectory(filepath.Dir(move.Target)); err != nil {
			return err
		}
		if err := atomicfile.SyncDirectory(ssl); err != nil {
			return err
		}
	}
	return CheckKeyLayout(root)
}
