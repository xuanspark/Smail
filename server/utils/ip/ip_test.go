package ip

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchPublicIPAcceptsPublicAddress(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("8.8.8.8\n"))
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	value, err := fetchPublicIP(client, request)
	if err != nil || value != "8.8.8.8" {
		t.Fatalf("unexpected public IP result: value=%q err=%v", value, err)
	}
}

func TestFetchPublicIPRejectsUnsafeResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "private", status: http.StatusOK, body: "192.168.1.10"},
		{name: "loopback", status: http.StatusOK, body: "127.0.0.1"},
		{name: "invalid", status: http.StatusOK, body: "not-an-ip"},
		{name: "large", status: http.StatusOK, body: strings.Repeat("1", maxIPResponseBytes+1)},
		{name: "status", status: http.StatusBadGateway, body: "8.8.8.8"},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(item.status)
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			request, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fetchPublicIP(client, request); err == nil {
				t.Fatal("unsafe public IP response was accepted")
			}
		})
	}
}

func TestFetchPublicIPRejectsPlainHTTP(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetchPublicIP(&http.Client{Timeout: time.Second}, request); err == nil {
		t.Fatal("plain HTTP public IP request was accepted")
	}
}

func TestFetchPublicIPForFamilyRequiresRequestedAddressFamily(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2001:db8::1\n"))
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := fetchPublicIPForFamily(server.Client(), request, true)
	if err != nil || value != "2001:db8::1" {
		t.Fatalf("unexpected IPv6 result: value=%q err=%v", value, err)
	}
	if _, err := fetchPublicIPForFamily(server.Client(), request, false); err == nil {
		t.Fatal("IPv6 result was accepted for IPv4 request")
	}
}
