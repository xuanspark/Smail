package config

import (
	"bytes"
	"crypto/tls"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPublishExternalTLSValidatesBeforeReplacingBundle(t *testing.T) {
	if runtime.GOOS != "linux" {
		if err := PublishExternalTLS(t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "tls.pem")); err == nil {
			t.Fatal("unsupported platform allowed publishing a private key")
		}
		t.Skip("Linux Nginx publishing requires POSIX permission checks")
	}
	root, source, output := t.TempDir(), t.TempDir(), t.TempDir()
	for _, directory := range []string{source, output} {
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	candidate := validRuntimeConfig()
	candidate.Root.Domains = DomainConfig{Primary: "example.com", Web: "web.example.com", Accepted: []string{"example.com"}}
	candidate.Root.HTTP = HTTPConfig{Port: 15184, TLSMode: HTTPSTLSModeProxy, TrustedProxyCIDRs: []string{"127.0.0.1/32"}}
	candidate.SSL.TLS = SSLConfig{Management: SSLManagementWebServer, Mode: ManagedSSLModeUpload, CustomDirectory: "ssl"}
	writeRuntimeFixture(t, root, candidate)
	key, certificate := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, "*.example.com")
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("example.com.crt", certificate)
	write("example.com.key", key)
	destination := filepath.Join(output, "tls.pem")
	if err := PublishExternalTLS(root, source, destination); err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair(bundle, bundle); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil || before.Mode().Perm() != 0600 {
		t.Fatal("published bundle must be private")
	}
	if err := PublishExternalTLS(root, source, destination); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(destination)
	if !os.SameFile(before, after) {
		t.Fatal("unchanged certificate must not be rewritten")
	}
	assertRejected := func() {
		t.Helper()
		if err := PublishExternalTLS(root, source, destination); err == nil {
			t.Fatal("invalid publication accepted")
		}
		preserved, err := os.ReadFile(destination)
		if err != nil || !bytes.Equal(bundle, preserved) {
			t.Fatal("failed publication replaced the previous bundle")
		}
	}
	write("example.com.key", []byte("invalid"))
	assertRejected()
	wrongKey, wrongCertificate := testManagedCertificate(t, "other.example", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048)
	write("example.com.key", wrongKey)
	write("example.com.crt", wrongCertificate)
	assertRejected()
	write("example.com.key", key)
	write("example.com.crt", certificate)
	if err := os.Chmod(filepath.Join(source, "example.com.key"), 0644); err != nil {
		t.Fatal(err)
	}
	assertRejected()
	if err := os.Chmod(filepath.Join(source, "example.com.key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PublishExternalTLS(root, source, filepath.Join(source, "example.com.crt")); err == nil {
		t.Fatal("source overwrite allowed")
	}
	candidate.Root.HTTP.TLSMode = HTTPSTLSModeDirect
	writeRuntimeFixture(t, root, candidate)
	assertRejected()
}

func TestExportProjectTLSKeepsOldBundleOnInvalidInput(t *testing.T) {
	if runtime.GOOS != "linux" {
		if err := ExportProjectTLS(t.TempDir(), filepath.Join(t.TempDir(), "tls.pem")); err == nil {
			t.Fatal("unsupported platform allowed exporting a private key")
		}
		t.Skip("Linux Nginx export requires POSIX permission checks")
	}
	root := t.TempDir()
	outputDirectory := t.TempDir()
	if err := os.Chmod(outputDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(outputDirectory, "tls.pem")
	runtime := validRuntimeConfig()
	runtime.Root.Domains = DomainConfig{Primary: "example.com", Web: "web.example.com", Accepted: []string{"example.com"}}
	key, certificate := testManagedCertificate(t, "example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, "*.example.com")
	ref, err := stageFlatSSLAtRoot(root, ManagedSSLModeDefault, key, certificate, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseManagedSSL(root, SSLConfig{CertificateRef: ref.CertificateRef})
	runtime.SSL.TLS = SSLConfig{Management: SSLManagementProject, Challenge: SSLChallengeDNS, Mode: ManagedSSLModeDefault, DefaultDirectory: "ssl", CertificateRef: ref.CertificateRef, PrivateKeyRef: ref.PrivateKeyRef}
	writeRuntimeFixture(t, root, runtime)
	if err := ExportProjectTLS(root, destination); err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair(bundle, bundle); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil || before.Mode().Perm() != 0600 {
		t.Fatal("export must be private")
	}
	if err := ExportProjectTLS(root, destination); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(destination)
	if !os.SameFile(before, after) {
		t.Fatal("unchanged certificate must not be rewritten")
	}
	for _, target := range []string{ref.PrivateKeyPath, ref.CertificatePath, destination} {
		if err := os.Chmod(target, 0644); err != nil {
			t.Fatal(err)
		}
		if err := ExportProjectTLS(root, destination); err == nil {
			t.Fatal("unsafe file permissions accepted")
		}
		if err := os.Chmod(target, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(outputDirectory, 0777); err != nil {
		t.Fatal(err)
	}
	if err := ExportProjectTLS(root, destination); err == nil {
		t.Fatal("writable output directory accepted")
	}
	if err := os.Chmod(outputDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(outputDirectory, link); err != nil {
		t.Fatal(err)
	}
	if err := ExportProjectTLS(root, filepath.Join(link, "tls.pem")); err == nil {
		t.Fatal("linked output directory accepted")
	}
	hardLink := filepath.Join(t.TempDir(), "linked.key")
	if err := os.Link(ref.PrivateKeyPath, hardLink); err != nil {
		t.Fatal(err)
	}
	if err := ExportProjectTLS(root, destination); err == nil {
		t.Fatal("hard-linked private key accepted")
	}
	if err := os.Remove(hardLink); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.PrivateKeyPath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ExportProjectTLS(root, destination); err == nil {
		t.Fatal("invalid key was exported")
	}
	preserved, _ := os.ReadFile(destination)
	if !bytes.Equal(bundle, preserved) {
		t.Fatal("failed export replaced valid bundle")
	}
	runtime.SSL.TLS.CertificateRef = "ssl/unmanaged.crt"
	runtime.SSL.TLS.PrivateKeyRef = "ssl/unmanaged.key"
	writeRuntimeFixture(t, root, runtime)
	if err := ExportProjectTLS(root, destination); err == nil {
		t.Fatal("unmanaged input accepted")
	}
	for _, mode := range []string{SSLManagementCustom, SSLManagementWebServer} {
		runtime.SSL.TLS.Management = mode
		writeRuntimeFixture(t, root, runtime)
		if err := ExportProjectTLS(root, destination); err != nil {
			t.Fatal(err)
		}
		preserved, _ := os.ReadFile(destination)
		if !bytes.Equal(bundle, preserved) {
			t.Fatal("external mode changed exported bundle")
		}
	}
}
