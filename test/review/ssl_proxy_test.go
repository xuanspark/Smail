package ssl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/signal"
)

type proxyBlockedTransport struct {
	calls int
}

func (transport *proxyBlockedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, errors.New("测试禁止外部网络请求")
}

// 旧版及分区配置均只在临时目录中测试，禁止连接真实 ACME 服务。
func TestAuditProxyDisablesAutomaticSSL(t *testing.T) {
	for _, sslType := range []string{config.SSLTypeAutoHTTP, config.SSLTypeAutoDNS} {
		t.Run(sslType, func(t *testing.T) {
			auditSSLRoot(t)
			cfg := &config.Config{
				TLSMode: config.HTTPSTLSModeProxy, HttpsEnabled: 2,
				SSLType: sslType, SetupState: config.SetupStateReady,
				Domain: "example.test", WebDomain: "mail.example.test",
			}
			if err := config.WriteConfig(cfg); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(config.ROOT_PATH, "config", "config.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			transport := &proxyBlockedTransport{}
			previousTransport, previousRestart := http.DefaultTransport, signal.RestartChan
			http.DefaultTransport = transport
			signal.RestartChan = make(chan bool, 1)
			t.Cleanup(func() {
				http.DefaultTransport, signal.RestartChan = previousTransport, previousRestart
			})
			selected, err := GetSSL()
			if err != nil || selected != config.SSLTypeUser {
				t.Fatalf("反代初始化必须选择外部证书: type=%s err=%v", selected, err)
			}
			if err := SetSSL(sslType, "", ""); !strings.Contains(UserCertificateErrorMessage(err), "反代") {
				t.Fatalf("反代模式未拒绝自动证书设置: %v", err)
			}
			for _, update := range []bool{false, true} {
				if err := GenSSL(update); !strings.Contains(UserCertificateErrorMessage(err), "反代") {
					t.Fatalf("反代模式未拒绝自动签发: update=%t err=%v", update, err)
				}
			}
			Update(true)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("反代模式改变了已有配置")
			}
			if transport.calls != 0 || len(signal.RestartChan) != 0 {
				t.Fatalf("反代模式触发网络请求或重启: requests=%d", transport.calls)
			}
			if _, err := os.Stat(filepath.Join(config.ROOT_PATH, "config", "ssl", "account_private.pem")); !os.IsNotExist(err) {
				t.Fatalf("反代模式不应生成 ACME 账户密钥: %v", err)
			}
		})
	}
}

func TestAuditProxyCertificateImportPreservesInternalHTTPSDisabled(t *testing.T) {
	auditSSLRoot(t)
	cfg := &config.Config{
		TLSMode: config.HTTPSTLSModeProxy, HttpsEnabled: 2,
		Domain: "mail.example.com", WebDomain: "mail.example.com",
	}
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	key, cert := writeTestCertificate(t, t.TempDir(), 2048)
	if err := SetSSL(config.SSLTypeUser, key, cert); err != nil {
		t.Fatal(err)
	}
	stored, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.HttpsEnabled != 2 || !stored.UsesProxyHTTPS() || stored.SSLType != config.SSLTypeUser {
		t.Fatal("导入邮件证书改变了反代 HTTPS 策略")
	}
	if _, err := config.LoadRuntimeTLSCertificate(stored); err != nil {
		t.Fatalf("反代模式邮件 TLS 证书必须仍可加载: %v", err)
	}
}

func TestAuditPartitionedProxyBlocksIssuance(t *testing.T) {
	expected, _ := auditMigratedSSL(t, config.SSLTypeAutoHTTP)
	// 保持证书分区不变，只在测试根文件上模拟管理员切换反代。
	path := filepath.Join(config.ROOT_PATH, "config", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 使用结构化配置读写，避免依赖序列化格式。
	var root config.RootConfig
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	root.HTTP.TLSMode = config.HTTPSTLSModeProxy
	root.HTTP.HTTPSEnabled = false
	root.HTTP.HTTPSPort = 0
	root.HTTP.TrustedProxyCIDRs = []string{"127.0.0.1/32"}
	data, err = json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AutomaticSSLEnabled() {
		t.Fatal("分区兼容快照仍允许反代自动续期")
	}
	if err := GenSSL(true); !strings.Contains(UserCertificateErrorMessage(err), "反代") {
		t.Fatalf("分区反代未拒绝自动签发: %v", err)
	}
	resource := auditCertificateChain(t, "imap.example.test", "smtp.imap.example.test", "pop.imap.example.test", "imap.imap.example.test")
	if err := writeCertificateFiles(expected, resource.PrivateKey, resource.Certificate, nil); !errors.Is(err, config.ErrGenerationConflict) {
		t.Fatalf("分区反代必须拒绝旧签发结果提交: %v", err)
	}
	// 使用同一临时分区配置，验证转交外部管理后仍可安全导入邮件证书。
	directory := t.TempDir()
	key, cert := filepath.Join(directory, "private.pem"), filepath.Join(directory, "certificate.pem")
	if err := os.WriteFile(key, resource.PrivateKey, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, resource.Certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetSSL(config.SSLTypeUser, key, cert); err != nil {
		t.Fatal(err)
	}
	stored, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.UsesProxyHTTPS() || stored.HttpsEnabled != 2 || stored.AutomaticSSLEnabled() {
		t.Fatal("分区证书导入改变了反代策略")
	}
	if _, err := config.LoadRuntimeTLSCertificate(stored); err != nil {
		t.Fatalf("分区配置导入后邮件 TLS 证书无法加载: %v", err)
	}
}
