package http_server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestRedirectToHTTPSPreservesPathQueryAndPort(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{WebDomain: "mail.example.com", HttpsPort: 8443}
	t.Cleanup(func() { config.Instance = old })

	request := httptest.NewRequest(http.MethodGet, "http://old.example/a%2Fb?x=1&y=2", nil)
	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, request)

	if recorder.Code != http.StatusPermanentRedirect {
		t.Fatalf("unexpected redirect status: %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "https://mail.example.com:8443/a%2Fb?x=1&y=2" {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}

func TestRedirectToHTTPSAllowsConfiguredIPHost(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{WebDomain: "192.0.2.10", HttpsPort: 443}
	t.Cleanup(func() { config.Instance = old })

	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, httptest.NewRequest(http.MethodGet, "http://old.example/Reset", nil))
	if location := recorder.Header().Get("Location"); recorder.Code != http.StatusPermanentRedirect || location != "https://192.0.2.10/Reset" {
		t.Fatalf("unexpected IP redirect: status=%d location=%q", recorder.Code, location)
	}
}

func TestRedirectToHTTPSRejectsUnsafeConfiguredHost(t *testing.T) {
	cases := []string{
		"",
		"user@evil.example",
		"mail.example.com/injected",
		"mail.example.com?next=evil.example",
		"mail.example.com#fragment",
	}
	for _, host := range cases {
		t.Run(host, func(t *testing.T) {
			old := config.Instance
			config.Instance = &config.Config{WebDomain: host, HttpsPort: 443}
			t.Cleanup(func() { config.Instance = old })

			recorder := httptest.NewRecorder()
			redirectToHTTPS(recorder, httptest.NewRequest(http.MethodGet, "http://old.example/path", nil))
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("unsafe host returned %d", recorder.Code)
			}
			if recorder.Header().Get("Location") != "" {
				t.Fatal("unsafe host produced a redirect")
			}
		})
	}
}
