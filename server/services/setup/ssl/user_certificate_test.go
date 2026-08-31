package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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

func TestIsAbsoluteUserCertificatePathRejectsWindowsPaths(t *testing.T) {
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
	if cfg.SSL.CustomDirectory != "config/ssl/managed/upload" {
		t.Fatalf("自定义受控目录写入错误: %s", cfg.SSL.CustomDirectory)
	}
}

func TestValidateUserCertificatePaths(t *testing.T) {
	oldRoot := config.ROOT_PATH
	oldExternalRoot := externalUserCertificateRoot
	root := t.TempDir()
	externalRoot := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	externalUserCertificateRoot = externalRoot
	t.Cleanup(func() {
		config.ROOT_PATH = oldRoot
		externalUserCertificateRoot = oldExternalRoot
	})

	keyPath, certPath := writeTestCertificate(t, root, 2048)
	if _, _, err := validateUserCertificatePaths(keyPath, certPath); err != nil {
		t.Fatalf("valid certificate was rejected: %v", err)
	}
	externalSourceRoot := t.TempDir()
	externalKey, externalCert := writeTestCertificate(t, externalSourceRoot, 2048)
	externalDirectory := filepath.Join(externalRoot, "example.com")
	if err := os.MkdirAll(externalDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	externalKeyTarget := filepath.Join(externalDirectory, "private.key")
	externalCertTarget := filepath.Join(externalDirectory, "public.crt")
	keyPEM, err := os.ReadFile(externalKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(externalCert)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalKeyTarget, keyPEM, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalCertTarget, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateUserCertificatePaths(externalKeyTarget, externalCertTarget); err != nil {
		t.Fatalf("external /ssl certificate was rejected: %v", err)
	}
	weakKey, weakCert := writeTestCertificate(t, root, 1024)
	if _, _, err := validateUserCertificatePaths(weakKey, weakCert); err == nil {
		t.Fatal("weak certificate was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.key")
	if err := os.WriteFile(outside, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateUserCertificatePaths(outside, certPath); err == nil {
		t.Fatal("outside path was accepted")
	}
}

func TestValidateUserCertificatePathsRejectsEscapingParentSymlink(t *testing.T) {
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
	if _, _, err := validateUserCertificatePaths(linkedKey, linkedCert); err == nil {
		t.Fatal("certificate paths escaped through a parent symlink")
	}
}
