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

func TestValidateUserCertificatePaths(t *testing.T) {
	oldRoot := config.ROOT_PATH
	root := t.TempDir()
	config.ROOT_PATH = root + string(os.PathSeparator)
	t.Cleanup(func() { config.ROOT_PATH = oldRoot })

	keyPath, certPath := writeTestCertificate(t, root, 2048)
	if _, _, err := validateUserCertificatePaths(keyPath, certPath); err != nil {
		t.Fatalf("valid certificate was rejected: %v", err)
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
