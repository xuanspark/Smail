package http_server

import (
	"crypto/tls"
	"io/fs"
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

func TestPublicRecoveryPageIsServedOnlyFromWebDomain(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://web.example.com/rest/"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("web recovery page returned %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-login-url="https://web.example.com/#/login"`) {
		t.Fatal("mail recovery page did not contain the configured web login URL")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("recovery page cache policy = %q", recorder.Header().Get("Cache-Control"))
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://mx.example.com/rest/"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("mail domain recovery page returned %d, want 404", recorder.Code)
	}
}

func TestPublicRecoveryServesUnderscorePrefixedBuildAssets(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}
	recoveryHandler, ok := handler.(*publicRecoveryHandler)
	if !ok {
		t.Fatalf("unexpected public recovery handler type %T", handler)
	}

	matches, err := fs.Glob(recoveryHandler.frontend, "assets/_plugin-vue_export-helper-*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("embedded frontend does not contain the Vue export helper asset")
	}

	recorder := httptest.NewRecorder()
	request := publicRecoveryRequest(http.MethodGet, "https://web.example.com/"+matches[0])
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("underscore-prefixed asset returned %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestPublicRecoveryPageRequiresConfiguredHTTPSPort(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	config.Instance.HttpsPort = 8443
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}

	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, publicRecoveryRequest(http.MethodGet, "https://web.example.com:8443/rest/"))
	if allowed.Code != http.StatusOK {
		t.Fatalf("configured HTTPS port returned %d, want %d", allowed.Code, http.StatusOK)
	}

	for _, target := range []string{
		"https://web.example.com/rest/",
		"https://web.example.com:9443/rest/",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, target))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("unexpected recovery page status %d for %q", recorder.Code, target)
			}
		})
	}
}

func TestPublicRecoveryUsesPublicPortBehindTLSProxy(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	config.Instance.HttpsPort = 8002
	config.Instance.PublicHTTPSPort = 443
	handler, err := newPublicRecoveryHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		authority string
		port      int
	}{
		{"standard public port", "web.example.com", 443},
		{"custom public port", "web.example.com:9443", 9443},
	} {
		t.Run(test.name, func(t *testing.T) {
			config.Instance.PublicHTTPSPort = test.port
			origin := "https://" + test.authority
			request := publicRecoveryRequest(http.MethodGet, origin+"/rest/")
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("Forwarded", "host=evil.example:8002;proto=https")
			request.Header.Set("X-Forwarded-Host", "evil.example")
			request.Header.Set("X-Forwarded-Port", "8002")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `data-login-url="`+origin+`/#/login"`) {
				t.Fatalf("恢复页未使用公网登录地址，状态 %d", recorder.Code)
			}
			recorder = httptest.NewRecorder()
			handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, origin+"/public-password-recovery"))
			if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != origin+"/rest/" {
				t.Fatal("恢复入口跳转携带了内部端口")
			}
			if !isPublicRecoveryOrigin(origin) || isPublicRecoveryOrigin("https://web.example.com:8002") || isPublicRecoveryOrigin("https://evil.example") {
				t.Fatal("恢复接口的来源校验没有使用公网端口")
			}
			for _, target := range []string{"https://web.example.com:8002/rest/", "https://evil.example/rest/"} {
				recorder = httptest.NewRecorder()
				handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, target))
				if recorder.Code != http.StatusNotFound {
					t.Fatal("接受了内部端口或非本站域名")
				}
			}
		})
	}
}

func TestPublicRecoveryRejectsInvalidPublicPort(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	for _, port := range []int{-1, 65536} {
		config.Instance.PublicHTTPSPort = port
		if _, ok := publicRecoveryBaseURL(); ok {
			t.Fatal("非法公网端口仍生成了恢复链接")
		}
		if isPublicRecoveryOrigin("https://web.example.com") {
			t.Fatal("非法公网端口回退为可信来源")
		}
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
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://web.example.com/rest/" {
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
			request := publicRecoveryRequest(http.MethodPost, "https://web.example.com/rest/api/recovery/begin")
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

	request := publicRecoveryRequest(http.MethodPost, "https://web.example.com/rest/api/recovery/begin")
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
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://web.example.com/smail-logo.png"))
	if recorder.Code != http.StatusOK || mainCalls != 0 {
		t.Fatalf("public recovery asset used main handler: status=%d calls=%d", recorder.Code, mainCalls)
	}
}

func TestPublicRecoveryDoesNotInterceptUnrelatedMainSitePaths(t *testing.T) {
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

	for _, target := range []string{
		"https://web.example.com/",
		"https://web.example.com/api/v1/user/info",
		"https://mx.example.com/assets/main.js",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, target))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("unrelated path %q returned %d, want %d", target, recorder.Code, http.StatusNoContent)
		}
	}
	if mainCalls != 3 {
		t.Fatalf("main handler calls = %d, want 3", mainCalls)
	}
}

func TestDisabledPublicRecoveryAssetsUseMainSite(t *testing.T) {
	configurePublicRecoveryRouteTest(t)
	config.Instance.ExternalRecoveryEnabled = false
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
	handler.ServeHTTP(recorder, publicRecoveryRequest(http.MethodGet, "https://web.example.com/smail-logo.png"))
	if recorder.Code != http.StatusNoContent || mainCalls != 1 {
		t.Fatalf("disabled recovery intercepted main asset: status=%d calls=%d", recorder.Code, mainCalls)
	}
}
