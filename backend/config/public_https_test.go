package config

import (
	"encoding/json"
	"testing"
)

func TestResolvePublicHTTPSPort(t *testing.T) {
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", "")
	t.Setenv("SMAIL_HTTPS_PORT", "")
	cases := []struct {
		name   string
		config HTTPConfig
		want   int
	}{
		{"default direct", HTTPConfig{}, 443},
		{"custom direct", HTTPConfig{HTTPSPort: 8443}, 8443},
		{"https upstream", HTTPConfig{HTTPSPort: 8002, PublicHTTPSPort: 443}, 443},
		{"custom public", HTTPConfig{HTTPSPort: 8002, PublicHTTPSPort: 9443}, 9443},
		{"http upstream", HTTPConfig{TLSMode: HTTPSTLSModeProxy}, 443},
		{"custom http upstream", HTTPConfig{TLSMode: HTTPSTLSModeProxy, PublicHTTPSPort: 9443}, 9443},
		{"invalid negative", HTTPConfig{PublicHTTPSPort: -1}, -1},
		{"invalid upper bound", HTTPConfig{PublicHTTPSPort: 65536}, 65536},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolvePublicHTTPSPort(test.config); got != test.want {
				t.Fatalf("公网端口为 %d，预期 %d", got, test.want)
			}
		})
	}
}

func TestPublicHTTPSPortValidationAndSerialization(t *testing.T) {
	for _, port := range []int{0, 1, 443, 8002, 65535} {
		value := HTTPConfig{Port: 8001, HTTPSPort: 8002, HTTPSEnabled: true, PublicHTTPSPort: port}
		if err := validateHTTP(value); err != nil {
			t.Fatal(err)
		}
		// 公网端口不是当前进程监听端口，不能参与本机端口冲突检查。
		if err := validateRuntimePorts(value, MailPorts{}); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var restored HTTPConfig
		if err := ParseStrict(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.PublicHTTPSPort != port || restored.HTTPSPort != 8002 {
			t.Fatalf("序列化丢失公网或内部端口：%s", data)
		}
	}
	for _, port := range []int{-1, 65536} {
		if validateHTTP(HTTPConfig{PublicHTTPSPort: port}) == nil {
			t.Fatalf("接受了非法公网端口 %d", port)
		}
	}
}

func TestPublicHTTPSPortRuntimeAccess(t *testing.T) {
	oldInstance, oldSnapshot, oldManager := Instance, CurrentConfig(), CurrentManager()
	t.Cleanup(func() {
		Instance = oldInstance
		PublishConfigSnapshot(oldSnapshot)
		SetRuntimeManager(oldManager)
	})
	SetRuntimeManager(nil)
	PublishConfigSnapshot(&Config{HttpPort: 8001, HttpsPort: 8002, PublicHTTPSPort: 443})
	if got := CurrentHTTPConfig(); got.PublicHTTPSPort != 443 || got.HTTPSPort != 8002 {
		t.Fatalf("旧配置运行时映射丢失端口：%+v", got)
	}
	runtime := validRuntimeConfig()
	runtime.Root.HTTP = HTTPConfig{Port: 8001, HTTPSPort: 8002, HTTPSEnabled: true, PublicHTTPSPort: 9443}
	SetRuntimeManager(&ConfigManager{snapshots: runtimeAccessSnapshotStore{runtime: runtime}})
	if got := CurrentHTTPConfig(); got.PublicHTTPSPort != 9443 || got.HTTPSPort != 8002 {
		t.Fatalf("分区配置运行时映射丢失端口：%+v", got)
	}
}

func TestWebAuthnUsesPublicHTTPSPort(t *testing.T) {
	root := RootConfig{Domains: DomainConfig{Web: "mail.example.com"}, HTTP: HTTPConfig{HTTPSPort: 8002, PublicHTTPSPort: 443}}
	for _, test := range []struct {
		port int
		want string
	}{{443, "https://mail.example.com"}, {9443, "https://mail.example.com:9443"}, {0, "https://mail.example.com:8002"}} {
		root.HTTP.PublicHTTPSPort = test.port
		security, err := ResolveWebAuthnSecurity(root)
		if err != nil || len(security.WebAuthnRPOrigins) != 1 || security.WebAuthnRPOrigins[0] != test.want {
			t.Fatalf("安全密钥默认来源不匹配：%+v，%v", security, err)
		}
	}
	root.HTTP.PublicHTTPSPort = 443
	root.Security.WebAuthnRPOrigins = []string{"https://mail.example.com:9443"}
	security, err := ResolveWebAuthnSecurity(root)
	if err != nil || security.WebAuthnRPOrigins[0] != root.Security.WebAuthnRPOrigins[0] {
		t.Fatal("公网端口覆盖了显式配置的安全密钥来源")
	}
}
