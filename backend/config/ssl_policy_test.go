package config

import (
	"testing"
	"time"
)

func TestSSLPolicyFollowsManagement(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil", want: SSLTypeUser},
		{name: "empty", cfg: &Config{}, want: SSLTypeUser},
		{name: "legacy_http", cfg: &Config{SSLType: SSLTypeAutoHTTP}, want: SSLTypeAutoHTTP},
		{name: "legacy_dns", cfg: &Config{SSLType: SSLTypeAutoDNS}, want: SSLTypeAutoDNS},
		{name: "legacy_manual", cfg: &Config{SSLType: SSLTypeUser}, want: SSLTypeUser},
		{name: "legacy_upload", cfg: &Config{SSLType: SSLTypeAutoHTTP, SSL: SSLConfig{Mode: ManagedSSLModeUpload}}, want: SSLTypeUser},
		{name: "legacy_default", cfg: &Config{SSLType: SSLTypeUser, SSL: SSLConfig{Mode: ManagedSSLModeDefault}}, want: SSLTypeAutoHTTP},
		{name: "project_http", cfg: &Config{SSLType: SSLTypeAutoDNS, SSL: SSLConfig{Management: SSLManagementProject, Challenge: SSLChallengeHTTP}}, want: SSLTypeAutoHTTP},
		{name: "project_dns", cfg: &Config{SSLType: SSLTypeUser, SSL: SSLConfig{Management: SSLManagementProject, Challenge: SSLChallengeDNS}}, want: SSLTypeAutoDNS},
		{name: "project_transition", cfg: &Config{SSLType: SSLTypeUser, SSL: SSLConfig{Management: SSLManagementProject, Mode: ManagedSSLModeUpload}}, want: SSLTypeAutoHTTP},
		{name: "project_legacy_dns", cfg: &Config{SSLType: SSLTypeAutoDNS, SSL: SSLConfig{Management: SSLManagementProject}}, want: SSLTypeAutoDNS},
		{name: "custom_stale_auto", cfg: &Config{SSLType: SSLTypeAutoHTTP, SSL: SSLConfig{Management: SSLManagementCustom, Mode: ManagedSSLModeDefault}}, want: SSLTypeUser},
		{name: "web_server_stale_auto", cfg: &Config{SSLType: SSLTypeAutoDNS, SSL: SSLConfig{Management: SSLManagementWebServer}}, want: SSLTypeUser},
		{name: "proxy_project", cfg: &Config{TLSMode: " PROXY ", SSL: SSLConfig{Management: SSLManagementProject}}, want: SSLTypeUser},
		{name: "invalid_management", cfg: &Config{SSLType: SSLTypeAutoHTTP, SSL: SSLConfig{Management: "invalid"}}, want: SSLTypeUser},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cfg.EffectiveSSLType(); got != test.want {
				t.Fatalf("证书策略错误：got=%s want=%s", got, test.want)
			}
			if test.cfg.AutomaticSSLEnabled() != (test.want != SSLTypeUser) {
				t.Fatal("自动续期开关与托管策略不一致")
			}
		})
	}
}

func TestSSLPolicyFollowsRuntimeTLSOverride(t *testing.T) {
	for _, test := range []struct {
		name      string
		stored    string
		override  string
		wantProxy bool
	}{
		{name: "bootstrap_proxy", override: HTTPSTLSModeProxy, wantProxy: true},
		{name: "direct_to_proxy", stored: HTTPSTLSModeDirect, override: " PROXY ", wantProxy: true},
		{name: "proxy_to_direct", stored: HTTPSTLSModeProxy, override: HTTPSTLSModeDirect},
		{name: "invalid_override", stored: HTTPSTLSModeProxy, override: "invalid", wantProxy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", test.override)
			cfg := &Config{TLSMode: test.stored, SSLType: SSLTypeAutoHTTP}
			if got := cfg.UsesProxyHTTPS(); got != test.wantProxy {
				t.Fatalf("proxy policy = %t, want %t", got, test.wantProxy)
			}
			if cfg.AutomaticSSLEnabled() == test.wantProxy {
				t.Fatal("certificate policy disagrees with effective TLS mode")
			}
		})
	}
}

func TestExternalCertificateReloadIgnoresLegacyDNSMode(t *testing.T) {
	previousRoot := ROOT_PATH
	ROOT_PATH = t.TempDir()
	t.Cleanup(func() { ROOT_PATH = previousRoot })
	key, cert := testManagedCertificate(t, "mail.example.com", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour), 2048)
	reference, err := ImportManagedSSL(ManagedSSLModeUpload, key, cert, nil, []string{"mail.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseStagedSSL(reference)
	for _, management := range []string{SSLManagementCustom, SSLManagementWebServer} {
		cfg := &Config{
			Domain: "mail.example.com", WebDomain: "mail.example.com", SSLType: SSLTypeAutoDNS,
			SSL: SSLConfig{Management: management, Mode: reference.Mode,
				CertificateRef: reference.CertificateRef, PrivateKeyRef: reference.PrivateKeyRef},
		}
		if _, err := loadValidatedRuntimeCertificate(cfg); err != nil {
			t.Fatalf("外部证书被旧 DNS 类型错误拒绝：management=%s err=%v", management, err)
		}
	}
}
