package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestProjectDNSRuntimeRequiresRootAndWildcard(t *testing.T) {
	for _, test := range []struct {
		name    string
		domains []string
		valid   bool
	}{
		{"root_and_wildcard", []string{"example.com", "*.example.com"}, true},
		{"wildcard_only", []string{"*.example.com"}, false},
		{"root_only", []string{"example.com"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousRoot := ROOT_PATH
			ROOT_PATH = t.TempDir()
			t.Cleanup(func() { ROOT_PATH = previousRoot })
			key, certificate := testManagedCertificate(t, test.domains[0], time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, test.domains[1:]...)
			// 故意不在准备阶段检查域名，模拟已有证书文件的加载与配置复验。
			reference, err := stageFlatSSLAtRoot(ROOT_PATH, ManagedSSLModeDefault, key, certificate, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ReleaseStagedSSL(reference)
			candidate := RuntimeConfig{Root: RootConfig{Domains: DomainConfig{
				Primary: "example.com", Web: "web.example.com", Accepted: []string{"example.com"},
			}}}
			candidate.SSL.TLS = SSLConfig{
				Management: SSLManagementProject, Challenge: SSLChallengeDNS,
				Mode: reference.Mode, CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef,
			}
			check := func(name string, err error) {
				t.Helper()
				if test.valid && err != nil {
					t.Errorf("%s 拒绝完整域名覆盖：%v", name, err)
				}
				if !test.valid && !errors.Is(err, ErrSSLCertificateDomain) {
					t.Errorf("%s 未拒绝不完整域名覆盖：%v", name, err)
				}
			}
			check("配置复验", validateCustomSSLReferences(ROOT_PATH, candidate))
			cfg := &Config{Domain: "example.com", WebDomain: "web.example.com", Domains: []string{"example.com"}, SSL: candidate.SSL.TLS}
			_, err = loadValidatedRuntimeCertificate(cfg)
			check("运行时证书", err)
			cfg.SSL.Management, cfg.SSL.Challenge, cfg.SSLType = "", "", SSLTypeAutoDNS
			_, err = loadValidatedRuntimeCertificate(cfg)
			check("旧自动 DNS 配置", err)
		})
	}
}

func TestProjectDNSExportRejectsMissingCoverageAndKeepsBundle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("证书导出需要 Linux 权限检查")
	}
	for _, domain := range []string{"*.example.com", "example.com"} {
		t.Run(domain, func(t *testing.T) {
			root, output := t.TempDir(), t.TempDir()
			if err := os.Chmod(output, 0700); err != nil {
				t.Fatal(err)
			}
			candidate := validRuntimeConfig()
			candidate.Root.Domains = DomainConfig{Primary: "example.com", Web: "web.example.com", Accepted: []string{"example.com"}}
			install := func(domains ...string) {
				t.Helper()
				key, certificate := testManagedCertificate(t, domains[0], time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, domains[1:]...)
				reference, err := stageFlatSSLAtRoot(root, ManagedSSLModeDefault, key, certificate, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { releaseManagedSSL(root, SSLConfig{CertificateRef: reference.CertificateRef}) })
				candidate.SSL.TLS = SSLConfig{
					Management: SSLManagementProject, Challenge: SSLChallengeDNS, Mode: ManagedSSLModeDefault,
					DefaultDirectory: "ssl", CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef,
				}
				writeRuntimeFixture(t, root, candidate)
			}
			install("example.com", "*.example.com")
			destination := filepath.Join(output, "tls.pem")
			if err := ExportProjectTLS(root, destination); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			install(domain)
			if err := ExportProjectTLS(root, destination); err == nil {
				t.Error("导出接受了不完整的根域名与泛域名覆盖")
			}
			after, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(before, after) {
				t.Error("失败导出替换了有效证书内容")
			}
			current, err := os.Stat(destination)
			if err != nil || !os.SameFile(info, current) || current.Mode().Perm() != 0600 {
				t.Error("失败导出改变了有效证书文件或权限")
			}
		})
	}
}

func TestProjectDNSReloadKeepsValidCertificateOnMissingRoot(t *testing.T) {
	previousRoot := ROOT_PATH
	previousConfig, previousPublished := publishedConfig.Load(), hasPublishedConfig.Load()
	runtimeManager.Lock()
	previousManager := runtimeManager.value
	runtimeManager.value = nil
	runtimeManager.Unlock()
	liveTLS.Lock()
	previousLiveRoot, previousCertificate := liveTLS.root, liveTLS.certificate
	previousChecked, previousWarned := liveTLS.checked, liveTLS.warned
	liveTLS.Unlock()
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() {
		ROOT_PATH = previousRoot
		publishedConfig.Store(previousConfig)
		hasPublishedConfig.Store(previousPublished)
		runtimeManager.Lock()
		runtimeManager.value = previousManager
		runtimeManager.Unlock()
		liveTLS.Lock()
		liveTLS.root, liveTLS.certificate = previousLiveRoot, previousCertificate
		liveTLS.checked, liveTLS.warned = previousChecked, previousWarned
		liveTLS.Unlock()
	})
	install := func(domains ...string) {
		t.Helper()
		key, certificate := testManagedCertificate(t, domains[0], time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 2048, domains[1:]...)
		reference, err := stageFlatSSLAtRoot(ROOT_PATH, ManagedSSLModeDefault, key, certificate, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ReleaseStagedSSL(reference) })
		PublishConfigSnapshot(&Config{
			Domain: "example.com", WebDomain: "web.example.com", Domains: []string{"example.com"},
			SSL: SSLConfig{Management: SSLManagementProject, Challenge: SSLChallengeDNS, Mode: reference.Mode,
				CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef},
		})
	}
	install("example.com", "*.example.com")
	configuration, err := NewRuntimeTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	before, err := configuration.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	install("*.example.com")
	if err := ReloadRuntimeTLSCertificate(); !errors.Is(err, ErrSSLCertificateDomain) {
		t.Fatalf("热加载未拒绝缺失根域名的新证书：%v", err)
	}
	after, err := configuration.GetCertificate(nil)
	if err != nil || after == nil || !bytes.Equal(before.Certificate[0], after.Certificate[0]) {
		t.Fatal("无效新证书替换了仍有效的内存证书")
	}
	install("example.com", "*.example.com")
	if err := ReloadRuntimeTLSCertificate(); err != nil {
		t.Fatal(err)
	}
	replaced, err := configuration.GetCertificate(nil)
	if err != nil || replaced == nil || bytes.Equal(before.Certificate[0], replaced.Certificate[0]) {
		t.Fatal("后续有效证书未能热加载")
	}
}
