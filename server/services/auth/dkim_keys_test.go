package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureDKIMKeyPairRepairsIncompleteFiles(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "config", "dkim")
	privatePath := filepath.Join(basePath, "dkim.priv")
	if err := os.MkdirAll(basePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privatePath, []byte("-----BEGIN "+"PRIVATE KEY-----\ntruncated"), 0644); err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(basePath, "dkim.public")
	if err := os.WriteFile(publicPath, []byte("stale-public-key"), 0644); err != nil {
		t.Fatal(err)
	}

	publicKey, err := ensureDKIMKeyPair(basePath, privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(publicKey, "v=DKIM1; k=rsa; p=") {
		t.Fatalf("DKIM 公钥格式无效: %q", publicKey)
	}
	privateData, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := parseDKIMPrivateKey(privateData)
	if err != nil {
		t.Fatalf("修复后的 DKIM 私钥无效: %v", err)
	}
	if privateKey.N.BitLen() < 2048 {
		t.Fatalf("DKIM 私钥长度不足: %d", privateKey.N.BitLen())
	}
	publicData, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(publicData) != publicKey {
		t.Fatal("DKIM 公钥文件与返回值不一致")
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{privatePath, publicPath} {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("DKIM 文件权限为 %o，期望 600", info.Mode().Perm())
			}
		}
		info, statErr := os.Stat(basePath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("DKIM 目录权限为 %o，期望 700", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dkim.") {
			t.Fatalf("残留 DKIM 临时文件: %s", entry.Name())
		}
	}
}

func TestEnsureDKIMKeyPairRepairsPublicKeyFromPrivateKey(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "dkim")
	privatePath := filepath.Join(basePath, "dkim.priv")
	firstPublicKey, err := ensureDKIMKeyPair(basePath, privatePath)
	if err != nil {
		t.Fatal(err)
	}
	privateBefore, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(basePath, "dkim.public"), []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}

	secondPublicKey, err := ensureDKIMKeyPair(basePath, privatePath)
	if err != nil {
		t.Fatal(err)
	}
	privateAfter, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(privateBefore, privateAfter) {
		t.Fatal("修复公钥时不应重新生成有效私钥")
	}
	if secondPublicKey != firstPublicKey {
		t.Fatal("从私钥恢复后的 DKIM 公钥不一致")
	}
}

func TestEnsureDKIMKeyPairRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "dkim")
	if err := os.Symlink(outside, basePath); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if _, err := ensureDKIMKeyPair(basePath, filepath.Join(basePath, "dkim.priv")); err == nil {
		t.Fatal("DKIM 符号链接目录应被拒绝")
	}
}

func TestEnsureDKIMKeyPairRejectsSymlinkPrivateKey(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "dkim")
	if err := os.MkdirAll(basePath, 0700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.key")
	original := []byte("outside-private-key")
	if err := os.WriteFile(outsidePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(basePath, "dkim.priv")
	if err := os.Symlink(outsidePath, privatePath); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if _, err := ensureDKIMKeyPair(basePath, privatePath); err == nil {
		t.Fatal("DKIM 私钥符号链接应被拒绝")
	}
	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outsideData, original) {
		t.Fatal("拒绝符号链接时不应修改外部文件")
	}
}
