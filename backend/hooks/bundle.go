package hooks

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var bundledPlugins fs.FS
var bundledRoot string
var bundleMu sync.Mutex

// SetBundledPlugins 注入编译时封装的插件；插件仍作为独立进程运行，不放宽生产隔离。
func SetBundledPlugins(files fs.FS) {
	bundledPlugins = files
}

func pluginRoot() (string, error) {
	bundleMu.Lock()
	defer bundleMu.Unlock()
	if bundledPlugins == nil {
		return filepath.Abs("./plugins")
	}
	if bundledRoot != "" {
		return bundledRoot, nil
	}
	entries, err := fs.ReadDir(bundledPlugins, ".")
	if err != nil || len(entries) != 3 {
		return "", errors.New("内置插件包不完整")
	}
	// 操作系统临时沙箱不属于部署文件，避免向 config 或 data 解包可执行程序。
	root, err := os.MkdirTemp("", "smail-bundled-")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			return "", errors.New("内置插件包含有无效文件")
		}
		data, err := fs.ReadFile(bundledPlugins, entry.Name())
		if err != nil || len(data) == 0 {
			return "", errors.New("内置插件内容不可用")
		}
		if err := writeBundledPlugin(filepath.Join(root, entry.Name()), data); err != nil {
			return "", err
		}
	}
	bundledRoot = root
	return root, nil
}

func writeBundledPlugin(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0700); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
