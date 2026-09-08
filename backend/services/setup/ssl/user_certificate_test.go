package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

func writeTestCertificate(t *testing.T, root string, bits int) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mail.example.com"},
		DNSNames:     []string{"mail.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "config", "ssl")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(directory, "private.key")
	certPath := filepath.Join(directory, "public.crt")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	return keyPath, certPath
}

func TestIsAbsoluteUserCertificatePathRecognizesWindowsPaths(t *testing.T) {
	cases := []string{
		`C:\outside\private.key`,
		`D:/outside/public.crt`,
		`\\server\share\private.key`,
		`//server/share/public.crt`,
	}
	for _, value := range cases {
		if !isAbsoluteUserCertificatePath(value) {
			t.Errorf("path %q was accepted", value)
		}
	}
}

func TestIsAbsoluteUserCertificatePathRejectsEmptyAndAcceptsRelative(t *testing.T) {
	if isAbsoluteUserCertificatePath("") {
		t.Fatal("empty path must not be treated as absolute")
	}
	if isAbsoluteUserCertificatePath(`config/ssl/upload/private.key`) {
		t.Fatal("controlled relative path must not be treated as absolute")
	}
}

func TestApplyManagedSSLReferenceDoesNotPersistLegacyPaths(t *testing.T) {
	cfg := &config.Config{
		SSLPrivateKeyPath: "config/ssl/private.key",
		SSLPublicKeyPath:  "config/ssl/public.crt",
	}
	reference := config.ManagedSSLReference{
		Mode:            config.ManagedSSLModeUpload,
		CertificateRef:  "config/ssl/managed/upload/current/public.crt",
		PrivateKeyRef:   "config/ssl/managed/upload/current/private.key",
		CertificatePath: `C:\server\config\ssl\managed\upload\current\public.crt`,
		PrivateKeyPath:  `C:\server\config\ssl\managed\upload\current\private.key`,
	}

	applyManagedSSLReference(cfg, reference)

	if cfg.SSLPrivateKeyPath != "" || cfg.SSLPublicKeyPath != "" {
		t.Fatal("新受控 SSL 配置不应保留旧聚合路径")
	}
	if cfg.SSL.Mode != config.ManagedSSLModeUpload || cfg.SSL.CertificateRef != reference.CertificateRef || cfg.SSL.PrivateKeyRef != reference.PrivateKeyRef {
		t.Fatalf("受控 SSL 内部引用写入错误: %+v", cfg.SSL)
	}
	if cfg.SSL.CustomDirectory != "data/ssl/managed/upload" {
		t.Fatalf("自定义受控目录写入错误: %s", cfg.SSL.CustomDirectory)
	}
}

func TestNormalizeUserCertificateImportError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		message string
	}{
		{name: "certificate mismatch", err: config.ErrSSLCertificateMismatch, message: "SSL 证书与私钥不匹配"},
		{name: "domain mismatch", err: fmt.Errorf("%w: web.xuan.com", config.ErrSSLCertificateDomain), message: "SSL 证书与已配置域名不匹配，请检查主域名、Web 域名和附加域名"},
		{name: "certificate expired", err: config.ErrSSLCertificateExpired, message: "SSL 证书无效、尚未生效或已经过期"},
		{name: "private key permission", err: config.ErrSSLPrivateKeyPermission, message: "SSL 私钥权限过于宽松，请限制为仅 Smail 运行用户可读取"},
		{name: "invalid material", err: config.ErrInvalidSSLMaterial, message: "SSL 证书或私钥文件无效"},
		{name: "unsafe file", err: config.ErrUnsafeSSLFile, message: "SSL 证书或私钥文件无效"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := UserCertificateErrorMessage(normalizeUserCertificateImportError(test.err))
			if message != test.message {
				t.Fatalf("证书错误提示不匹配: got %q, want %q", message, test.message)
			}
		})
	}

	unknown := fmt.Errorf("unknown certificate import error")
	if normalized := normalizeUserCertificateImportError(unknown); normalized != unknown {
		t.Fatalf("未知错误不应被替换: %v", normalized)
	}
}

func TestValidateUserCertificatePaths(t *testing.T) {
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() {
		config.ROOT_PATH = oldRoot
	})

	keyPath, certPath := writeTestCertificate(t, root, 2048)
	if _, _, err := validateUserCertificatePaths(keyPath, certPath); err != nil {
		t.Fatalf("valid certificate was rejected: %v", err)
	}
	externalSourceRoot := t.TempDir()
	externalKey, externalCert := writeTestCertificate(t, externalSourceRoot, 2048)
	if _, _, err := validateUserCertificatePaths(externalKey, externalCert); err != nil {
		t.Fatalf("certificate in an arbitrary directory was rejected: %v", err)
	}
	relativeKey, err := filepath.Rel(root, externalKey)
	if err != nil {
		t.Fatal(err)
	}
	relativeCert, err := filepath.Rel(root, externalCert)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateUserCertificatePaths(relativeKey, relativeCert); err != nil {
		t.Fatalf("certificate in an arbitrary relative directory was rejected: %v", err)
	}
	weakKey, weakCert := writeTestCertificate(t, root, 1024)
	if _, _, err := validateUserCertificatePaths(weakKey, weakCert); err == nil {
		t.Fatal("weak certificate was accepted")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.key")
	if err := os.WriteFile(invalid, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateUserCertificatePaths(invalid, certPath); err == nil {
		t.Fatal("invalid certificate data was accepted")
	}
}

func TestValidateUserCertificatePathsAcceptsParentSymlink(t *testing.T) {
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })

	sslDirectory := filepath.Join(root, "config", "ssl")
	if err := os.MkdirAll(sslDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	outsideRoot := t.TempDir()
	outsideKey, outsideCert := writeTestCertificate(t, outsideRoot, 2048)
	linkPath := filepath.Join(sslDirectory, "linked")
	if err := os.Symlink(filepath.Dir(outsideKey), linkPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	linkedKey := filepath.Join(linkPath, filepath.Base(outsideKey))
	linkedCert := filepath.Join(linkPath, filepath.Base(outsideCert))
	if _, _, err := validateUserCertificatePaths(linkedKey, linkedCert); err != nil {
		t.Fatalf("certificate paths through a parent symlink were rejected: %v", err)
	}
}
