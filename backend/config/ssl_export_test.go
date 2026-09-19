package config

import (
	"bytes"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportProjectTLSKeepsOldBundleOnInvalidInput(t *testing.T) {
	root := t.TempDir()
	outputDirectory := t.TempDir()
	if err := os.Chmod(outputDirectory, 0700); err != nil { t.Fatal(err) }
	destination := filepath.Join(outputDirectory, "tls.pem")
	runtime := validRuntimeConfig()
	runtime.Root.Domains = DomainConfig{Primary: "example.com", Web: "web.example.com", Accepted: []string{"example.com"}}
	key, certificate := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, "*.example.com")
	ref, err := stageFlatSSLAtRoot(root, ManagedSSLModeDefault, key, certificate, nil, nil)
	if err != nil { t.Fatal(err) }
	defer releaseManagedSSL(root, SSLConfig{CertificateRef: ref.CertificateRef})
	runtime.SSL.TLS = SSLConfig{Management: SSLManagementProject, Challenge: SSLChallengeDNS, Mode: ManagedSSLModeDefault, DefaultDirectory: "ssl", CertificateRef: ref.CertificateRef, PrivateKeyRef: ref.PrivateKeyRef}
	writeRuntimeFixture(t, root, runtime)
	if err := ExportProjectTLS(root, destination); err != nil { t.Fatal(err) }
	bundle, err := os.ReadFile(destination)
	if err != nil { t.Fatal(err) }
	if _, err := tls.X509KeyPair(bundle, bundle); err != nil { t.Fatal(err) }
	if err := ExportProjectTLS(root, destination); err != nil { t.Fatal(err) }
	if err := os.WriteFile(ref.PrivateKeyPath, []byte("invalid"), 0600); err != nil { t.Fatal(err) }
	if err := ExportProjectTLS(root, destination); err == nil { t.Fatal("invalid key was exported") }
	preserved, _ := os.ReadFile(destination)
	if !bytes.Equal(bundle, preserved) { t.Fatal("failed export replaced valid bundle") }
	runtime.SSL.TLS.CertificateRef = "ssl/unmanaged.crt"
	runtime.SSL.TLS.PrivateKeyRef = "ssl/unmanaged.key"
	writeRuntimeFixture(t, root, runtime)
	if err := ExportProjectTLS(root, destination); err == nil { t.Fatal("unmanaged input accepted") }
	for _, mode := range []string{SSLManagementCustom, SSLManagementWebServer} {
		runtime.SSL.TLS.Management = mode
		writeRuntimeFixture(t, root, runtime)
		if err := ExportProjectTLS(root, destination); err != nil { t.Fatal(err) }
		preserved, _ := os.ReadFile(destination)
		if !bytes.Equal(bundle, preserved) { t.Fatal("external mode changed exported bundle") }
	}
}
