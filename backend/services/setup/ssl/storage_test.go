package ssl

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestWriteCertificateBundleUsesRestrictedAtomicStorage(t *testing.T) {
	privateKey, certificate := readTestCertificatePair(t)
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })
	basePath := filepath.Join(root, "data", "ssl")
	cfg := &config.Config{
		SSLPrivateKeyPath: filepath.Join(basePath, "private.key"),
		SSLPublicKeyPath:  filepath.Join(basePath, "public.crt"),
	}
	issuer := []byte("issuer-certificate")
	if err := writeCertificateBundle(cfg, privateKey, certificate, issuer); err != nil {
		t.Fatal(err)
	}
	if err := writeCertificateBundle(cfg, privateKey, certificate, issuer); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]byte{
		cfg.SSLPrivateKeyPath:                     privateKey,
		cfg.SSLPublicKeyPath:                      certificate,
		filepath.Join(basePath, "issuerCert.crt"): issuer,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, expected) {
			t.Fatalf("SSL 文件内容不完整: %s", path)
		}
		if runtime.GOOS != "windows" {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("SSL 文件权限为 %o，期望 600", info.Mode().Perm())
			}
		}
	}
	if runtime.GOOS != "windows" {
		for _, directory := range []string{filepath.Join(root, "data"), basePath} {
			info, err := os.Stat(directory)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0700 {
				t.Fatalf("SSL 目录权限为 %o，期望 700", info.Mode().Perm())
			}
		}
	}
	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".private.key-") || strings.HasPrefix(entry.Name(), ".public.crt-") || strings.HasPrefix(entry.Name(), ".issuerCert.crt-") {
			t.Fatalf("残留 SSL 临时文件: %s", entry.Name())
		}
	}
}

func TestWriteCertificateBundleRejectsSymlinkDirectory(t *testing.T) {
	privateKey, certificate := readTestCertificatePair(t)
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })
	configDirectory := filepath.Join(root, "data")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	basePath := filepath.Join(configDirectory, "ssl")
	if err := os.Symlink(outside, basePath); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	cfg := &config.Config{
		SSLPrivateKeyPath: filepath.Join(basePath, "private.key"),
		SSLPublicKeyPath:  filepath.Join(basePath, "public.crt"),
	}
	if err := writeCertificateBundle(cfg, privateKey, certificate, nil); err == nil {
		t.Fatal("SSL 符号链接目录应被拒绝")
	}
}

func TestWriteCertificateBundleRejectsSymlinkFile(t *testing.T) {
	privateKey, certificate := readTestCertificatePair(t)
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })
	basePath := filepath.Join(root, "data", "ssl")
	if err := os.MkdirAll(basePath, 0700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.key")
	original := []byte("outside-private-key")
	if err := os.WriteFile(outsidePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(basePath, "private.key")
	if err := os.Symlink(outsidePath, privatePath); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	cfg := &config.Config{
		SSLPrivateKeyPath: privatePath,
		SSLPublicKeyPath:  filepath.Join(basePath, "public.crt"),
	}
	if err := writeCertificateBundle(cfg, privateKey, certificate, nil); err == nil {
		t.Fatal("SSL 私钥符号链接应被拒绝")
	}
	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outsideData, original) {
		t.Fatal("拒绝 SSL 符号链接时不应修改外部文件")
	}
}

func TestWriteCertificateBundleRejectsMismatchedPair(t *testing.T) {
	privateKey, _ := readTestCertificatePair(t)
	_, otherCertificate := readTestCertificatePair(t)
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })
	basePath := filepath.Join(root, "data", "ssl")
	cfg := &config.Config{
		SSLPrivateKeyPath: filepath.Join(basePath, "private.key"),
		SSLPublicKeyPath:  filepath.Join(basePath, "public.crt"),
	}
	if err := writeCertificateBundle(cfg, privateKey, otherCertificate, nil); err == nil {
		t.Fatal("不匹配的证书和私钥应被拒绝")
	}
}

func readTestCertificatePair(t *testing.T) ([]byte, []byte) {
	t.Helper()
	keyPath, certPath := writeTestCertificate(t, t.TempDir(), 2048)
	privateKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, certificate
}
