package res_init

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestDirInitUsesRuntimeRoot(t *testing.T) {
	root, working := t.TempDir(), t.TempDir()
	t.Chdir(working)
	previous := config.ROOT_PATH
	config.ROOT_PATH = root
	t.Cleanup(func() { config.ROOT_PATH = previous })

	for i := 0; i < 2; i++ {
		if err := dirInit(); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"config", "config/dkim", "config/ssl"} {
			info, err := os.Stat(filepath.Join(root, name))
			if err != nil || !info.IsDir() {
				t.Fatalf("运行目录未创建: %s", name)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(working, "config")); !os.IsNotExist(err) {
		t.Fatal("初始化不应在工作目录创建另一份配置")
	}
}

func TestDirInitPreservesExistingFiles(t *testing.T) {
	root := t.TempDir()
	previous := config.ROOT_PATH
	config.ROOT_PATH = root
	t.Cleanup(func() { config.ROOT_PATH = previous })
	if err := dirInit(); err != nil {
		t.Fatal(err)
	}
	files := []string{"config/config.json", "config/smail.db", "config/credential.key", "config/dkim/dkim.priv", "config/ssl/private.key"}
	data := []byte("existing-runtime-fixture")
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := dirInit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatalf("初始化改变了已有运行文件: %s", name)
		}
	}
}

func TestDirInitRejectsFileAsDirectory(t *testing.T) {
	root := t.TempDir()
	previous := config.ROOT_PATH
	config.ROOT_PATH = root
	t.Cleanup(func() { config.ROOT_PATH = previous })
	if err := os.Mkdir(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "dkim"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := dirInit(); err == nil {
		t.Fatal("普通文件不能冒充运行目录")
	}
}

func TestDirInitRejectsSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	previous := config.ROOT_PATH
	config.ROOT_PATH = root
	t.Cleanup(func() { config.ROOT_PATH = previous })
	if err := os.Symlink(outside, filepath.Join(root, "config")); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}
	if err := dirInit(); err == nil {
		t.Fatal("运行配置目录不能通过符号链接逃逸")
	}
	if _, err := os.Stat(filepath.Join(outside, "dkim")); !os.IsNotExist(err) {
		t.Fatal("拒绝符号链接前不应写入外部目录")
	}
}
