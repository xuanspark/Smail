package clientip

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		peer    string
		trusted []string
		headers []string
		want    string
	}{
		{"direct ipv4", "192.0.2.1:1234", nil, nil, "192.0.2.1"},
		{"direct ipv6", "[2001:db8::1]:1234", nil, nil, "2001:db8::1"},
		{"mapped ipv4", "[::ffff:192.0.2.1]:1234", nil, nil, "192.0.2.1"},
		{"host compatibility", "proxy.example:443", nil, nil, "proxy.example"},
		{"no implicit loopback trust", "127.0.0.1:1234", nil, []string{"192.0.2.1"}, "127.0.0.1"},
		{"untrusted ignores spoof", "192.0.2.1:1234", []string{"127.0.0.1/32"}, []string{"198.51.100.1"}, "192.0.2.1"},
		{"single proxy", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"192.0.2.1"}, "192.0.2.1"},
		{"mapped proxy", "[::ffff:127.0.0.1]:1234", []string{"127.0.0.1/32"}, []string{"::ffff:192.0.2.1"}, "192.0.2.1"},
		{"ipv6 proxy", "[::1]:1234", []string{"::1/128"}, []string{"2001:0db8::1"}, "2001:db8::1"},
		{"untrusted intermediate", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"198.51.100.5, 192.0.2.1"}, "192.0.2.1"},
		{"trusted intermediate", "127.0.0.1:1234", []string{"127.0.0.1/32", "10.0.0.0/24"}, []string{"192.0.2.1, 10.0.0.2"}, "192.0.2.1"},
		{"all trusted", "127.0.0.1:1234", []string{"127.0.0.0/8"}, []string{"127.0.0.2"}, "127.0.0.2"},
		{"empty header", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{""}, "127.0.0.1"},
		{"missing header", "127.0.0.1:1234", []string{"127.0.0.1/32"}, nil, "127.0.0.1"},
		{"duplicate header", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"192.0.2.1", "192.0.2.2"}, "127.0.0.1"},
		{"port rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"192.0.2.1:80"}, "127.0.0.1"},
		{"zone rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"fe80::1%eth0"}, "127.0.0.1"},
		{"unspecified rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"0.0.0.0"}, "127.0.0.1"},
		{"mapped unspecified rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"::ffff:0.0.0.0"}, "127.0.0.1"},
		{"mapped multicast rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"::ffff:224.0.0.1"}, "127.0.0.1"},
		{"multicast rejected", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"ff02::1"}, "127.0.0.1"},
		{"malformed chain", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"unknown, 192.0.2.1"}, "127.0.0.1"},
		{"empty chain element", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{"192.0.2.1,"}, "127.0.0.1"},
		{"invalid configuration", "127.0.0.1:1234", []string{"127.0.0.1/32", "invalid"}, []string{"192.0.2.1"}, "127.0.0.1"},
		{"oversized header", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{strings.Repeat(" ", 4096) + "192.0.2.1"}, "127.0.0.1"},
		{"too many hops", "127.0.0.1:1234", []string{"127.0.0.1/32"}, []string{strings.Repeat("192.0.2.1,", 32) + "192.0.2.1"}, "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://mail.example.com/", nil)
			request.RemoteAddr = test.peer
			for _, header := range test.headers {
				request.Header.Add("X-Forwarded-For", header)
			}
			request.Header.Set("X-Real-IP", "203.0.113.9")
			request.Header.Set("Forwarded", "for=203.0.113.9")
			if got := Resolve(request, test.trusted); got != test.want {
				t.Fatalf("解析结果为 %q，预期 %q", got, test.want)
			}
			if request.RemoteAddr != test.peer {
				t.Fatal("解析过程修改了真实连接来源")
			}
		})
	}
}

func TestResolveNilRequest(t *testing.T) {
	if Resolve(nil, []string{"127.0.0.1/32"}) != "" {
		t.Fatal("空请求应返回空地址")
	}
}
