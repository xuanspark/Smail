package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestImportManagedSSLUsesImmutableRootFiles(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)

	first, err := ImportManagedSSL(ManagedSSLModeDefault, privateKeyPEM, certificatePEM, nil, []string{"mail.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseStagedSSL(first)
	if filepath.IsAbs(first.CertificateRef) || filepath.IsAbs(first.PrivateKeyRef) {
		t.Fatal("SSL 内部引用不应为绝对路径")
	}
	if !flatSSLPair(ROOT_PATH, first.CertificatePath, first.PrivateKeyPath) {
		t.Fatalf("内部引用不符合预期: %+v", first)
	}
	resolved, err := ResolveManagedSSLReferences(SSLConfig{Mode: ManagedSSLModeDefault, DefaultDirectory: "config/ssl", CertificateRef: first.CertificateRef, PrivateKeyRef: first.PrivateKeyRef})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CertificatePath != first.CertificatePath || resolved.PrivateKeyPath != first.PrivateKeyPath {
		t.Fatal("解析后的受控路径与导入结果不一致")
	}

	secondKey, secondCertificate := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	second, err := ImportManagedSSL(ManagedSSLModeDefault, secondKey, secondCertificate, nil, []string{"mail.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseStagedSSL(second)
	if second.CertificatePath == first.CertificatePath {
		t.Fatal("新证书必须使用独立文件，不能覆盖正在使用的材料")
	}
	currentData, err := os.ReadFile(second.CertificatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(currentData) == string(certificatePEM) {
		t.Fatal("current 目录没有切换到新证书")
	}
	previousData, err := os.ReadFile(first.CertificatePath)
	if err != nil || string(previousData) != string(certificatePEM) {
		t.Fatal("暂存新证书改变了原证书")
	}
}

func TestResolveRuntimeSSLPathsUsesManagedReferences(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	reference, err := ImportManagedSSL(ManagedSSLModeDefault, privateKeyPEM, certificatePEM, nil, []string{"mail.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{SSL: SSLConfig{Mode: ManagedSSLModeDefault, CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef}}
	certificatePath, privateKeyPath, err := ResolveRuntimeSSLPaths(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if certificatePath != reference.CertificatePath || privateKeyPath != reference.PrivateKeyPath {
		t.Fatalf("resolved paths = %q, %q", certificatePath, privateKeyPath)
	}
	if _, err := LoadRuntimeTLSCertificate(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRuntimeSSLPathsRejectsPartialManagedConfiguration(t *testing.T) {
	_, _, err := ResolveRuntimeSSLPaths(&Config{SSL: SSLConfig{Mode: ManagedSSLModeDefault, CertificateRef: "config/ssl/managed/default/current/public.crt"}})
	if !errors.Is(err, ErrInvalidSSLReference) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidSSLReference)
	}
}

func TestResolveRuntimeSSLPathsValidatesLegacyFiles(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	directory := filepath.Join(t.TempDir(), "legacy-ssl")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	privateKeyPath := filepath.Join(directory, "private.key")
	certificatePath := filepath.Join(directory, "public.crt")
	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificatePath, certificatePEM, 0600); err != nil {
		t.Fatal(err)
	}
	resolvedCertificate, resolvedPrivate, err := ResolveRuntimeSSLPaths(&Config{SSLPublicKeyPath: certificatePath, SSLPrivateKeyPath: privateKeyPath})
	if err != nil {
		t.Fatal(err)
	}
	if resolvedCertificate != certificatePath || resolvedPrivate != privateKeyPath {
		t.Fatalf("resolved paths = %q, %q", resolvedCertificate, resolvedPrivate)
	}
}

func TestResolveManagedSSLReferencesRejectsUnsafeReferences(t *testing.T) {
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	reference, err := ImportManagedSSL(ManagedSSLModeUpload, privateKeyPEM, certificatePEM, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := []SSLConfig{
		{Mode: ManagedSSLModeUpload, CertificateRef: filepath.Join(filepath.Dir(reference.CertificateRef), "..", "public.crt"), PrivateKeyRef: reference.PrivateKeyRef},
		{Mode: ManagedSSLModeUpload, CertificateRef: filepath.Join(t.TempDir(), "public.crt"), PrivateKeyRef: reference.PrivateKeyRef},
		{Mode: ManagedSSLModeUpload, CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.CertificateRef},
		{Mode: ManagedSSLModeUpload, CertificateRef: `C:\outside\public.crt`, PrivateKeyRef: reference.PrivateKeyRef},
		{Mode: ManagedSSLModeUpload, CertificateRef: `\\server\share\public.crt`, PrivateKeyRef: reference.PrivateKeyRef},
	}
	for index, candidate := range unsafe {
		if _, err := ResolveManagedSSLReferences(candidate); err == nil {
			t.Fatalf("用例 %d 不应接受缺失、不配对或不安全的文件", index)
		}
	}
}

func TestResolveManagedSSLReferencesRejectsSymlinkAndBroadPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("当前测试依赖 Unix 文件权限和符号链接")
	}
	oldRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = oldRoot })
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	reference, err := ImportManagedSSL(ManagedSSLModeDefault, privateKeyPEM, certificatePEM, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(reference.PrivateKeyPath, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveManagedSSLReferences(SSLConfig{Mode: ManagedSSLModeDefault, CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef}); !errors.Is(err, ErrSSLPrivateKeyPermission) {
		t.Fatalf("私钥权限 error = %v, want %v", err, ErrSSLPrivateKeyPermission)
	}
	if err := os.Chmod(reference.PrivateKeyPath, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.crt")
	if err := os.WriteFile(outside, certificatePEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(reference.CertificatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, reference.CertificatePath); err != nil {
		t.Skipf("当前环境不支持符号链接测试: %v", err)
	}
	if _, err := ResolveManagedSSLReferences(SSLConfig{Mode: ManagedSSLModeDefault, CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef}); !errors.Is(err, ErrUnsafeSSLFile) {
		t.Fatalf("符号链接 error = %v, want %v", err, ErrUnsafeSSLFile)
	}
}

func TestValidateSSLMaterialRejectsDomainExpiryAndWeakKeys(t *testing.T) {
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	if err := ValidateSSLMaterial(privateKeyPEM, certificatePEM, nil, []string{"other.example.com"}); !errors.Is(err, ErrSSLCertificateDomain) {
		t.Fatalf("域名不匹配 error = %v, want %v", err, ErrSSLCertificateDomain)
	}
	expiredKey, expiredCertificate := testManagedCertificate(t, "mail.example.com", time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour), 2048)
	if err := ValidateSSLMaterial(expiredKey, expiredCertificate, nil, nil); !errors.Is(err, ErrSSLCertificateExpired) {
		t.Fatalf("过期证书 error = %v, want %v", err, ErrSSLCertificateExpired)
	}
	weakKey, weakCertificate := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 1024)
	if err := ValidateSSLMaterial(weakKey, weakCertificate, nil, nil); !errors.Is(err, ErrInvalidSSLMaterial) {
		t.Fatalf("弱密钥 error = %v, want %v", err, ErrInvalidSSLMaterial)
	}
}

func TestSSLCertificateDomainsIncludesWebAndMailServiceDomains(t *testing.T) {
	domains := SSLCertificateDomains("example.com", "mail.example.com", []string{"example.com"})
	for _, want := range []string{"example.com", "mail.example.com", "smtp.example.com", "imap.example.com", "pop.example.com"} {
		found := false
		for _, got := range domains {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("certificate domains %v do not include %q", domains, want)
		}
	}
}

func TestPrepareCustomSSLPathsKeepsDirectFilesWithoutCopying(t *testing.T) {
	root := t.TempDir()
	previousRoot := ROOT_PATH
	ROOT_PATH = root
	t.Cleanup(func() { ROOT_PATH = previousRoot })
	external := t.TempDir()
	privateKeyPEM, certificatePEM := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	privateKeyPath := filepath.Join(external, "privkey.pem")
	certificatePath := filepath.Join(external, "fullchain.pem")
	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificatePath, certificatePEM, 0600); err != nil {
		t.Fatal(err)
	}
	runtime := validRuntimeConfig()
	runtime.Root.Domains = DomainConfig{Primary: "mail.example.com", Web: "mail.example.com"}
	runtime.SSL.TLS = SSLConfig{
		Management:       SSLManagementCustom,
		Mode:             ManagedSSLModeUpload,
		DefaultDirectory: "config/ssl",
		CustomDirectory:  "config/ssl",
		CertificateRef:   certificatePath,
		PrivateKeyRef:    privateKeyPath,
	}

	material, err := prepareCustomSSLPaths(root, &runtime)
	if err != nil {
		t.Fatalf("校验外部证书失败: %v", err)
	}
	if runtime.SSL.TLS.CertificateRef != filepath.ToSlash(certificatePath) || runtime.SSL.TLS.PrivateKeyRef != filepath.ToSlash(privateKeyPath) {
		t.Fatal("仅校验阶段应保留规范化的直接路径")
	}
	managedRoot := filepath.Join(root, "data", "ssl", "managed", "upload")
	if _, err := os.Stat(managedRoot); !os.IsNotExist(err) {
		t.Fatalf("仅校验阶段不应创建受管目录，error = %v", err)
	}

	if err := material.stage(root, &runtime); err != nil {
		t.Fatalf("保存外部证书失败: %v", err)
	}
	if runtime.SSL.TLS.CertificateRef != filepath.ToSlash(certificatePath) || runtime.SSL.TLS.PrivateKeyRef != filepath.ToSlash(privateKeyPath) {
		t.Fatal("保存后应继续引用原始文件")
	}
	if _, err := os.Stat(managedRoot); !os.IsNotExist(err) {
		t.Fatal("保存自定义证书不应创建副本")
	}
	if _, err := ResolveManagedSSLReferences(runtime.SSL.TLS); err != nil {
		t.Fatalf("受管证书引用无法解析: %v", err)
	}
}

func TestValidateSSLMaterialAcceptsBundledIssuerWithoutDuplicateChainEntry(t *testing.T) {
	privateKeyPEM, certificatePEM, issuerPEM := testChainedCertificate(t)
	certificateBundle := append(append([]byte{}, certificatePEM...), issuerPEM...)
	if err := ValidateSSLMaterial(privateKeyPEM, certificateBundle, issuerPEM, []string{"mail.example.com"}); err != nil {
		t.Fatalf("bundled issuer was rejected as a duplicate chain entry: %v", err)
	}
}

func testManagedCertificate(t *testing.T, domain string, notBefore, notAfter time.Time, bits int, additionalDomains ...string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: domain},
		DNSNames:              append([]string{domain}, additionalDomains...),
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testChainedCertificate(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	issuerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	issuerTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "Smail Test Issuer"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuerTemplate, issuerTemplate, &issuerKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(101),
		Subject:               pkix.Name{CommonName: "mail.example.com"},
		DNSNames:              []string{"mail.example.com"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, issuerTemplate, &leafKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER})
}
