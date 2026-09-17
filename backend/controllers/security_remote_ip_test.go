package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestRemoteIPNormalizesIPv4AndIPv6Addresses(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "ipv4 with port", addr: "192.0.2.10:12345", want: "192.0.2.10"},
		{name: "ipv6 with port", addr: "[2001:db8::1]:12345", want: "2001:db8::1"},
		{name: "ipv6 without port", addr: "2001:0db8:0:0:0:0:0:1", want: "2001:db8::1"},
		{name: "bracketed ipv6 without port", addr: "[2001:db8::1]", want: "2001:db8::1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/", nil)
			request.RemoteAddr = test.addr
			if got := remoteIP(request); got != test.want {
				t.Fatalf("remote IP = %q, want %q", got, test.want)
			}
			if got := loginRemoteHost(request); got != test.want {
				t.Fatalf("login remote host = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRemoteIPTrustedProxySeparatesLoginLimits(t *testing.T) {
	oldConfig, oldManager := config.Instance, config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{
		TLSMode:           config.HTTPSTLSModeDirect,
		TrustedProxyCIDRs: []string{"127.0.0.1/32"},
	}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})
	first := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", nil)
	first.RemoteAddr = "127.0.0.1:1234"
	first.Header.Set("X-Forwarded-For", "192.0.2.10")
	second := first.Clone(first.Context())
	second.Header.Set("X-Forwarded-For", "192.0.2.11")
	limiter := newLoginLimiter(1)
	limiter.recordFailure(limiter.ipKey(first))
	if !limiter.blocked(limiter.ipKey(first)) || limiter.blocked(limiter.ipKey(second)) {
		t.Fatal("可信代理后的两个客户端共享了 IP 限流状态")
	}
	if limiter.key(first, "user") == limiter.key(second, "user") {
		t.Fatal("账号与 IP 组合限流没有区分客户端")
	}
	if remoteIP(first) != "192.0.2.10" || loginRemoteHost(first) != remoteIP(first) {
		t.Fatal("安全验证与登录限流的客户端地址不一致")
	}
	second.RemoteAddr = "198.51.100.10:4321"
	second.Header.Set("X-Forwarded-For", "192.0.2.10")
	if remoteIP(second) != "198.51.100.10" {
		t.Fatal("非可信连接能够伪造安全验证来源")
	}
	if first.RemoteAddr != "127.0.0.1:1234" {
		t.Fatal("代理解析修改了真实连接来源")
	}
}

func TestRemoteIPPreservesNonIPHostAndHandlesMissingAddress(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/", nil)
	request.RemoteAddr = "proxy.example:443"
	if got := remoteIP(request); got != "proxy.example" {
		t.Fatalf("remote host = %q, want proxy.example", got)
	}
	request.RemoteAddr = ""
	if got := remoteIP(request); got != "" {
		t.Fatalf("empty remote address = %q, want empty string", got)
	}
	if got := loginRemoteHost(request); got != "unknown" {
		t.Fatalf("missing login remote host = %q, want unknown", got)
	}
}
