package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigFileUsesRestrictedAtomicStorage(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir() + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })

	if err := writeConfigFile("config.json", []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile("config.json", []byte(`{"version":2}`)); err != nil {
		t.Fatal(err)
	}
	data, err := readConfigFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"version":2}` {
		t.Fatalf("配置文件内容为 %q，期望原子替换后的内容", data)
	}
	directory := filepath.Join(ROOT_PATH, "config")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config.json-") {
			t.Fatalf("残留配置临时文件: %s", entry.Name())
		}
	}
	if runtime.GOOS != "windows" {
		fileInfo, statErr := os.Stat(filepath.Join(directory, "config.json"))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if fileInfo.Mode().Perm() != 0600 {
			t.Fatalf("配置文件权限为 %o，期望 600", fileInfo.Mode().Perm())
		}
		directoryInfo, statErr := os.Stat(directory)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if directoryInfo.Mode().Perm() != 0700 {
			t.Fatalf("配置目录权限为 %o，期望 700", directoryInfo.Mode().Perm())
		}
	}
}

func TestConfigStorageRejectsSymlinkDirectory(t *testing.T) {
	oldRoot := ROOT_PATH
	root := t.TempDir()
	ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "config")); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if err := writeConfigFile("config.json", []byte(`{"safe":true}`)); err == nil {
		t.Fatal("配置符号链接目录应被拒绝")
	}
}

func TestConfigStorageRejectsSymlinkFile(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir() + string(os.PathSeparator)
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	directory, err := ensureSecureConfigDirectory()
	if err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.json")
	original := []byte(`{"outside":true}`)
	if err := os.WriteFile(outsidePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(directory, "config.json")); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if err := writeConfigFile("config.json", []byte(`{"safe":true}`)); err == nil {
		t.Fatal("配置符号链接文件应被拒绝")
	}
	if _, err := readConfigFile("config.json"); err == nil {
		t.Fatal("配置读取不应跟随符号链接")
	}
	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outsideData, original) {
		t.Fatal("拒绝配置符号链接时不应修改外部文件")
	}
}
