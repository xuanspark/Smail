package config

import (
	"bytes"
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadOrCreateACMEPrivateKeyRepairsIncompleteFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "config", "ssl")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "account_private.pem")
	if err := os.WriteFile(path, []byte("-----BEGIN EC PRIVATE"+" KEY-----\ntruncated"), 0644); err != nil {
		t.Fatal(err)
	}

	privateKey, created, err := loadOrCreateACMEPrivateKey(directory, "account_private.pem")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("不完整的 ACME 私钥应被原子替换")
	}
	assertP256PrivateKey(t, privateKey)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseACMEPrivateKey(data)
	if err != nil {
		t.Fatalf("修复后的 ACME 私钥无效: %v", err)
	}
	assertP256PrivateKey(t, parsed)

	loaded, createdAgain, err := loadOrCreateACMEPrivateKey(directory, "account_private.pem")
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain {
		t.Fatal("有效的 ACME 私钥不应重复生成")
	}
	if loaded.D.Cmp(privateKey.D) != 0 {
		t.Fatal("重复加载的 ACME 私钥不一致")
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("ACME 私钥权限为 %o，期望 600", info.Mode().Perm())
		}
		directoryInfo, statErr := os.Stat(directory)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if directoryInfo.Mode().Perm() != 0700 {
			t.Fatalf("ACME 私钥目录权限为 %o，期望 700", directoryInfo.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".account_private.pem-") {
			t.Fatalf("残留 ACME 私钥临时文件: %s", entry.Name())
		}
	}
}

func TestLoadOrCreateACMEPrivateKeyRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "ssl")
	if err := os.Symlink(outside, directory); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if _, _, err := loadOrCreateACMEPrivateKey(directory, "account_private.pem"); err == nil {
		t.Fatal("ACME 私钥符号链接目录应被拒绝")
	}
}

func TestLoadOrCreateACMEPrivateKeyRejectsSymlinkFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "ssl")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.pem")
	original := []byte("outside-acme-private-key")
	if err := os.WriteFile(outsidePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "account_private.pem")
	if err := os.Symlink(outsidePath, path); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if _, _, err := loadOrCreateACMEPrivateKey(directory, "account_private.pem"); err == nil {
		t.Fatal("ACME 私钥符号链接文件应被拒绝")
	}
	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outsideData, original) {
		t.Fatal("拒绝符号链接时不应修改外部文件")
	}
}

func assertP256PrivateKey(t *testing.T, privateKey *ecdsa.PrivateKey) {
	t.Helper()
	if privateKey == nil || privateKey.Curve == nil || privateKey.Curve.Params().Name != "P-256" {
		t.Fatal("ACME 私钥必须使用 P-256 曲线")
	}
}
