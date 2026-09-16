package http_server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func configurePublicRecoveryRouteTest(t *testing.T) {
	t.Helper()
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	oldDevelopment := config.DevelopmentMode
	config.SetRuntimeManager(nil)
	config.DevelopmentMode = false
	config.Instance = &config.Config{
		SetupState:              config.SetupStateReady,
		Domain:                  "mx.example.com",
		WebDomain:               "web.example.com",
		ExternalRecoveryEnabled: true,
		HttpsPort:               443,
	}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
		config.DevelopmentMode = oldDevelopment
	})
}

func publicRecoveryRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.TLS = &tls.ConnectionState{}
	return request
}

func TestPublicRecoveryPageIsServedOnlyFromMailDomain(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://mx.example.com/rest/"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("mail recovery page returned %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-login-url="https://web.example.com/#/login"`) {
		t.Fatal("mail recovery page did not contain the configured web login URL")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("recovery page cache policy = %q", recorder.Header().Get("Cache-Control"))
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://web.example.com/rest/"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("web domain recovery page returned %d, want 404", recorder.Code)
	}
}

func TestPublicRecoveryLoginEntryRedirectsOnlyFromWebDomain(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://web.example.com/public-password-recovery"))
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://mx.example.com/rest/" {
		t.Fatalf("unexpected login entry redirect: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://evil.example/public-password-recovery"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("foreign login entry returned %d, want 404", recorder.Code)
	}
}

func TestPublicRecoveryAPIRejectsCookiesAndCrossSiteRequests(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		prepare func(*http.Request)
	}{
		{name: "session cookie", prepare: func(request *http.Request) { request.AddCookie(&http.Cookie{Name: "smail_session", Value: "session"}) }},
		{name: "csrf header", prepare: func(request *http.Request) { request.Header.Set("X-CSRF-Token", "csrf") }},
		{name: "cross site", prepare: func(request *http.Request) { request.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{name: "foreign origin", prepare: func(request *http.Request) { request.Header.Set("Origin", "https://evil.example") }},
		{name: "wrong content type", prepare: func(request *http.Request) { request.Header.Set("Content-Type", "text/plain") }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := publicRecoveryRequest(http.MethodPost, "https://mx.example.com/rest/api/recovery/begin")
			request.Header.Set("Content-Type", "application/json")
			test.prepare(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("invalid public recovery request returned %d", recorder.Code)
			}
		})
	}
}

func TestPublicRecoveryAPIUsesNoMainSessionHandler(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	publicRecovery, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}
	mainCalls := 0
	handler := publicRecoveryOrSessionHandler(publicRecovery, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainCalls++
		w.WriteHeader(http.StatusNoContent)
	}))

	request := publicRecoveryRequest(http.MethodPost, "https://mx.example.com/rest/api/recovery/begin")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if mainCalls != 0 {
		t.Fatal("public recovery request entered the main session handler")
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("public recovery request set a cookie: %q", recorder.Header().Get("Set-Cookie"))
	}
}

func TestPublicRecoveryStaticAssetsBypassMainSessionHandler(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	publicRecovery, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}
	mainCalls := 0
	handler := publicRecoveryOrSessionHandler(publicRecovery, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainCalls++
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://mx.example.com/smail-logo.png"))
	if recorder.Code != http.StatusOK || mainCalls != 0 {
		t.Fatalf("public recovery asset used main handler: status=%d calls=%d", recorder.Code, mainCalls)
	}
}
