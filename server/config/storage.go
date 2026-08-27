package config

import (
	"errors"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
)

const maxConfigFileSize = 2 << 20

func readConfigFile(name string) ([]byte, error) {
	directory, err := ensureSecureConfigDirectory()
	if err != nil {
		return nil, err
	}
	if filepath.Base(name) != name || name == "." || name == "" {
		return nil, errors.New("invalid configuration file path")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxConfigFileSize {
		return nil, errors.New("invalid configuration file")
	}
	return root.ReadFile(name)
}

func writeConfigFile(name string, data []byte) error {
	directory, err := ensureSecureConfigDirectory()
	if err != nil {
		return err
	}
	if filepath.Base(name) != name || name == "." || name == "" {
		return errors.New("invalid configuration file path")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, statErr := root.Lstat(name); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("invalid configuration file")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return atomicfile.WriteRoot(root, name, data, 0600)
}

func ensureSecureConfigDirectory() (string, error) {
	directory := filepath.Join(ROOT_PATH, "config")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("invalid configuration directory")
	}
	// #nosec G302 -- 配置目录包含凭据和私钥，只允许当前服务账户访问。
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}
