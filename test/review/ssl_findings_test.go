package ssl

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"github.com/go-acme/lego/v4/certificate"
)

// 构造根、中间和叶证书，模拟 ACME 的完整链响应；不请求真实 CA。
func auditCertificateChain(t *testing.T, domain string, extraDomains ...string) certificate.Resource {
	t.Helper()
	createKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	rootKey, issuerKey, leafKey := createKey(), createKey(), createKey()
	makeTemplate := func(serial int64, name string, ca bool) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
			IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		}
	}
	root := makeTemplate(1, "Audit Root", true)
	issuer := makeTemplate(2, "Audit Intermediate", true)
	leaf := makeTemplate(3, domain, false)
	leaf.DNSNames = []string{domain}
	leaf.DNSNames = append(leaf.DNSNames, extraDomains...)
	leaf.KeyUsage = x509.KeyUsageDigitalSignature
	leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	encode := func(template, parent *x509.Certificate, key, signingKey *ecdsa.PrivateKey) []byte {
		der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signingKey)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	issuerPEM := encode(issuer, root, issuerKey, rootKey)
	leafPEM := encode(leaf, issuer, leafKey, issuerKey)
	privateDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	return certificate.Resource{
		PrivateKey:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}),
		Certificate: append(leafPEM, issuerPEM...), IssuerCertificate: issuerPEM,
	}
}

func auditSSLRoot(t *testing.T) {
	t.Helper()
	oldRoot, oldConfig, oldSnapshot := config.ROOT_PATH, config.Instance, config.CurrentConfig()
	oldManager := config.CurrentManager()
	config.ROOT_PATH = t.TempDir()
	config.SetRuntimeManager(nil)
	t.Cleanup(func() {
		config.ROOT_PATH, config.Instance = oldRoot, oldConfig
		config.PublishConfigSnapshot(oldSnapshot)
		config.SetRuntimeManager(oldManager)
	})
}

func TestAuditACMEBundledChainMustBeImportable(t *testing.T) {
	auditSSLRoot(t)
	resource := auditCertificateChain(t, "imap.example.test")
	if _, err := tls.X509KeyPair(resource.Certificate, resource.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateSSLMaterial(resource.PrivateKey, resource.Certificate, nil, []string{"imap.example.test"}); err != nil {
		t.Fatalf("正常完整链对照失败: %v", err)
	}
	cfg := &config.Config{Domain: "imap.example.test", SSLType: config.SSLTypeAutoHTTP}
	err := writeCertificateFiles(cfg, resource.PrivateKey, resource.Certificate, resource.IssuerCertificate)
	t.Logf("bundle_only_valid=true bundle_plus_issuer_write_error=%v", err)
	if err != nil {
		t.Fatalf("ACME Bundle=true 的完整链与单独 issuer 字段不应导致正常签发结果被拒绝: %v", err)
	}
}

func TestAuditFreshWildcardCertificateMustNotRequireRenewal(t *testing.T) {
	auditSSLRoot(t)
	cfg := &config.Config{Domain: "example.test", WebDomain: "web.example.test", SSLType: config.SSLTypeAutoDNS}
	resource := auditCertificateChain(t, "example.test", "*.example.test")
	// 不再附加 issuer，以隔离完整链重复问题和续期判断问题。
	if err := writeCertificateFiles(cfg, resource.PrivateKey, resource.Certificate, nil); err != nil {
		t.Fatal(err)
	}
	pair, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("imap.example.test"); err != nil {
		t.Fatalf("泛域名覆盖 IMAP 的前提不成立: %v", err)
	}
	days, _, mismatch, err := CheckSSLCrtInfo()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("days_remaining=%d imap_hostname_valid=true name_match_fail=%t renewal_predicate=%t", days, mismatch, days < 30 || mismatch)
	if days < 30 || mismatch {
		t.Fatal("新签发且覆盖服务域名的泛域名证书不应立即触发续期")
	}
}

// 只在临时根目录建立受控证书和真实分区配置，不接触运行中的实例。
func auditMigratedSSL(t *testing.T, mode string) (*config.Config, []byte) {
	t.Helper()
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	t.Cleanup(func() { config.SetRuntimeManager(oldManager) })
	auditSSLRoot(t)
	resource := auditCertificateChain(t, "imap.example.test", "smtp.imap.example.test", "pop.imap.example.test", "imap.imap.example.test")
	// 旧配置的证书位于 data/ssl；新导入接口已改用平铺文件，不能混合作为旧版夹具。
	certificateDirectory := filepath.Join(config.ROOT_PATH, "data", "ssl", "managed", "default", "current")
	if err := os.MkdirAll(certificateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certificateDirectory, "public.crt"), resource.Certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certificateDirectory, "private.key"), resource.PrivateKey, 0600); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"logLevel":          "info",
		"domain":            "imap.example.test",
		"webDomain":         "imap.example.test",
		"dbType":            "sqlite",
		"dbDSN":             "./config/review.db",
		"isInit":            true,
		"sslType":           mode,
		"SSLPublicKeyPath":  "data/ssl/managed/default/current/public.crt",
		"SSLPrivateKeyPath": "data/ssl/managed/default/current/private.key",
		"httpPort":          80,
		"httpsPort":         443,
		"httpsEnabled":      1,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(config.ROOT_PATH, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.ROOT_PATH, "config", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.MigrateLegacyConfig(config.ROOT_PATH, config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsSetupReady() {
		t.Fatal("分区配置未达到可用状态，无法验证续期路径")
	}
	return cfg, resource.Certificate
}

func TestAuditPartitionedRenewalMustCommitBeforeSwitch(t *testing.T) {
	cfg, original := auditMigratedSSL(t, config.SSLTypeAutoHTTP)
	loaded, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(config.ROOT_PATH, "config", "config.json")
	before, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	resource := auditCertificateChain(t, "imap.example.test", "smtp.imap.example.test", "pop.imap.example.test", "imap.imap.example.test")
	writeErr := writeCertificateFiles(cfg, resource.PrivateKey, resource.Certificate, nil)
	after, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, _, err := config.ResolveRuntimeSSLPaths(persisted)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	currentPair, err := config.LoadRuntimeTLSCertificate(persisted)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("write_error=%v root_unchanged=%t current_changed=%t current_is_new=%t loaded_tls_is_old=%t",
		writeErr, bytes.Equal(before, after), !bytes.Equal(original, current), bytes.Equal(current, resource.Certificate), !bytes.Equal(loaded.Certificate[0], currentPair.Certificate[0]))
	if writeErr != nil {
		t.Fatal("分区配置的自动续期仍调用旧写入接口，失败后受控证书已经切换")
	}
}

func TestAuditPartitionedMigrationMustPreserveDNSChallengeMode(t *testing.T) {
	cfg, _ := auditMigratedSSL(t, config.SSLTypeAutoDNS)
	t.Logf("before_ssl_type=%s after_ssl_type=%s before_dns_challenge=true after_http_challenge=%t", config.SSLTypeAutoDNS, cfg.SSLType, cfg.SSLType == config.SSLTypeAutoHTTP)
	if cfg.SSLType != config.SSLTypeAutoDNS {
		t.Fatal("自动 DNS 配置迁移后不应静默改成 HTTP 挑战")
	}
}
