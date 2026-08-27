package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
