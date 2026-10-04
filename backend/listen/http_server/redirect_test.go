package http_server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/session"
	"github.com/alexedwards/scs/v2"
)

func TestRedirectToHTTPSPreservesPathQueryAndPort(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{WebDomain: "mail.example.com", HttpsPort: 8443}
	t.Cleanup(func() { config.Instance = old })

	request := httptest.NewRequest(http.MethodGet, "http://mail.example.com/a%2Fb?x=1&y=2", nil)
	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, request)

	if recorder.Code != http.StatusPermanentRedirect {
		t.Fatalf("unexpected redirect status: %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "https://mail.example.com:8443/a%2Fb?x=1&y=2" {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}

func TestProxyHTTPHandlerServesBusinessRoutesWithoutRedirect(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	oldSession := session.Instance
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{WebDomain: "example.com", TLSMode: config.HTTPSTLSModeProxy}
	session.Instance = scs.New()
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
		session.Instance = oldSession
	})

	handler, err := newHTTPHandler(config.CurrentHTTPConfig())
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	if recorder.Code == http.StatusPermanentRedirect || recorder.Code == http.StatusTemporaryRedirect {
		t.Fatalf("proxy HTTP handler redirected the request: %d", recorder.Code)
	}
}

func TestDirectHTTPHandlerKeepsHTTPSRedirect(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{WebDomain: "example.com", HttpsEnabled: 1, HttpsPort: 443}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})

	handler, err := newHTTPHandler(config.CurrentHTTPConfig())
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/inbox", nil))
	if recorder.Code != http.StatusPermanentRedirect || recorder.Header().Get("Location") != "https://example.com/inbox" {
		t.Fatalf("unexpected direct HTTP redirect: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestRedirectToHTTPSAllowsConfiguredIPHost(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{WebDomain: "192.0.2.10", HttpsPort: 443}
	t.Cleanup(func() { config.Instance = old })

	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, httptest.NewRequest(http.MethodGet, "http://192.0.2.10/Reset", nil))
	if location := recorder.Header().Get("Location"); recorder.Code != http.StatusPermanentRedirect || location != "https://192.0.2.10/Reset" {
		t.Fatalf("unexpected IP redirect: status=%d location=%q", recorder.Code, location)
	}
}

func TestRedirectToHTTPSKeepsConfiguredMailDomain(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{Domain: "mx.example.com", WebDomain: "web.example.com", HttpsPort: 443}
	t.Cleanup(func() { config.Instance = old })

	request := httptest.NewRequest(http.MethodGet, "http://mx.example.com/rest/?next=1", nil)
	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, request)

	if recorder.Code != http.StatusPermanentRedirect || recorder.Header().Get("Location") != "https://mx.example.com/rest/?next=1" {
		t.Fatalf("unexpected mail domain redirect: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestRedirectToHTTPSRejectsUnknownRequestHost(t *testing.T) {
	old := config.Instance
	config.Instance = &config.Config{Domain: "mx.example.com", WebDomain: "web.example.com", HttpsPort: 443}
	t.Cleanup(func() { config.Instance = old })

	recorder := httptest.NewRecorder()
	redirectToHTTPS(recorder, httptest.NewRequest(http.MethodGet, "http://evil.example/rest/", nil))
	if recorder.Code != http.StatusUpgradeRequired || recorder.Header().Get("Location") != "" {
		t.Fatalf("unknown host was redirected: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
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
