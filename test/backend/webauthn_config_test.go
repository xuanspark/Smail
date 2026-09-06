package config

import (
	"reflect"
	"testing"
)

func TestWebAuthnDefaultsUseConfiguredSiteOnly(t *testing.T) {
	t.Setenv("SMAIL_HTTPS_PORT", "")
	for _, item := range []struct {
		name string
		http HTTPConfig
		want string
	}{
		{"legacy", HTTPConfig{}, "https://mail.example.com"},
		{"direct", HTTPConfig{HTTPSPort: 9443, HTTPSEnabled: true}, "https://mail.example.com:9443"},
		{"proxy", HTTPConfig{Port: 8080, TLSMode: HTTPSTLSModeProxy}, "https://mail.example.com"},
	} {
		t.Run(item.name, func(t *testing.T) {
			root := RootConfig{Domains: DomainConfig{Web: "mail.example.com", AccessHosts: []string{"untrusted.example.net"}}, HTTP: item.http}
			got, err := ResolveWebAuthnSecurity(root)
			if err != nil {
				t.Fatal(err)
			}
			if got.WebAuthnRPID != "mail.example.com" || got.WebAuthnRPDisplayName != "Smail" || !reflect.DeepEqual(got.WebAuthnRPOrigins, []string{item.want}) {
				t.Fatalf("站点默认值错误：%+v", got)
			}
			if root.Security.WebAuthnRPID != "" || len(root.Security.WebAuthnRPOrigins) != 0 {
				t.Fatal("解析默认值改写了原始配置")
			}
		})
	}
}

func TestWebAuthnExplicitConfigurationIsPreserved(t *testing.T) {
	root := RootConfig{Domains: DomainConfig{Web: "mail.example.com"}, Security: SecurityConfig{
		WebAuthnRPID: "example.com", WebAuthnRPDisplayName: "企业邮箱", WebAuthnRPOrigins: []string{"https://mail.example.com:8443"},
	}}
	got, err := ResolveWebAuthnSecurity(root)
	if err != nil || !reflect.DeepEqual(got, root.Security) {
		t.Fatalf("显式配置被覆盖：%v", err)
	}
	got.WebAuthnRPOrigins[0] = "https://changed.example.com"
	if root.Security.WebAuthnRPOrigins[0] != "https://mail.example.com:8443" {
		t.Fatal("解析结果与原始配置共用来源切片")
	}
}

func TestWebAuthnRejectsUnsafeConfiguredOrigins(t *testing.T) {
	for _, origin := range []string{
		"http://mail.example.com", "https://attacker.example.net", "https://badexample.com",
		"https://mail.example.com/path", "https://mail.example.com?", "https://mail.example.com#",
		"https://user@mail.example.com", "https://mail.example.com:65536", "https://*.example.com", "",
		"https://mail.example.com:", "https://mail.example.com:0", "https://mail.example.com:invalid",
	} {
		t.Run(origin, func(t *testing.T) {
			root := RootConfig{Domains: DomainConfig{Web: "mail.example.com"}, Security: SecurityConfig{
				WebAuthnRPID: "example.com", WebAuthnRPOrigins: []string{origin},
			}}
			if _, err := ResolveWebAuthnSecurity(root); err == nil {
				t.Fatal("不安全的显式来源未被拒绝")
			}
		})
	}
	if _, err := ResolveWebAuthnSecurity(RootConfig{}); err == nil {
		t.Fatal("缺少站点配置时不应猜测信赖方")
	}
}

func TestWebAuthnDefaultsRespectExplicitLocalDevelopmentOrigin(t *testing.T) {
	root := RootConfig{Domains: DomainConfig{Web: "localhost"}, Security: SecurityConfig{
		WebAuthnRPOrigins: []string{"http://localhost:15173"},
	}}
	if _, err := ResolveWebAuthnSecurity(root); err != nil {
		t.Fatalf("显式 localhost 来源被拒绝：%v", err)
	}
}

func TestRuntimeValidationRejectsInvalidWebAuthnBeforeSaving(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.Root.Security.WebAuthnRPOrigins = []string{"https://attacker.example.net"}
	if err := ValidateRuntimeConfig(runtime, RuntimeValidationOptions{HasSuperAdmin: true, Production: true}); err == nil {
		t.Fatal("候选配置接受了不属于站点信赖方的来源")
	}
}
