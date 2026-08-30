package http_server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"github.com/alexedwards/scs/v2"
)

func TestRouterRejectsGetForPostOnlyReadStateEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/email/detail", "/api/email/list", "/api/user/info", "/api/user/list", "/api/rule/get"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s returned %d, want %d", endpoint, recorder.Code, http.StatusMethodNotAllowed)
		}
	}
}

func configureProductionOriginTest(t *testing.T) {
	t.Helper()
	oldConfig := config.Instance
	oldDevelopmentMode := config.DevelopmentMode
	config.Instance = &config.Config{WebDomain: "mail.example.com"}
	config.DevelopmentMode = false
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.DevelopmentMode = oldDevelopmentMode
	})
}

func TestCSRFRejectsCrossOriginCookieRequest(t *testing.T) {
	configureProductionOriginTest(t)
	oldInit := config.IsInit
	oldSession := session.Instance
	defer func() {
		config.IsInit = oldInit
		session.Instance = oldSession
	}()
	config.IsInit = false
	session.Instance = scs.New()
	session.Instance.Cookie.Name = "smail_session"

	called := false
	handler := contextIterceptor(controllers.HandlerFunc(func(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
		called = true
		response.NewSuccessResponse("ok").FPrint(w)
	}))

	request := httptest.NewRequest(http.MethodPost, "http://mail.example.com/api/setup", nil)
	request.Host = "mail.example.com"
	request.Header.Set("Origin", "https://evil.example.com")
	request.AddCookie(&http.Cookie{Name: "smail_session", Value: "value"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if called || result.ErrorNo != response.ParamsError {
		t.Fatalf("cross-origin request was not rejected: called=%v result=%+v", called, result)
	}
}

func TestCSRFAllowsSameOriginCookieRequest(t *testing.T) {
	configureProductionOriginTest(t)
	oldInit := config.IsInit
	oldSession := session.Instance
	defer func() {
		config.IsInit = oldInit
		session.Instance = oldSession
	}()
	config.IsInit = false
	session.Instance = scs.New()
	session.Instance.Cookie.Name = "smail_session"

	called := false
	handler := contextIterceptor(controllers.HandlerFunc(func(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
		called = true
		response.NewSuccessResponse("ok").FPrint(w)
	}))

	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/setup", nil)
	request.Host = "mail.example.com"
	request.Header.Set("Origin", "https://mail.example.com")
	request.Header.Set(csrf.HeaderName, "csrf-value")
	request.AddCookie(&http.Cookie{Name: "smail_session", Value: "value"})
	request.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: "csrf-value"})
	loaded, err := session.Instance.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	session.Instance.Put(loaded, csrf.SessionKey, "csrf-value")
	request = request.WithContext(loaded)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("same-origin request was rejected")
	}
}

func TestCSRFRejectsMatchingForgedOriginAndHost(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://evil.example.com/api/settings", nil)
	request.Host = "evil.example.com"
	request.Header.Set("Origin", "https://evil.example.com")
	if sameOriginRequest(request) {
		t.Fatal("forged origin and host were trusted")
	}
}

func TestLoginRequestRejectsCrossSiteOrigin(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", bytes.NewBufferString(`{"account":"test","password":"test"}`))
	request.Host = "mail.example.com"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	if loginRequestAllowed(request) {
		t.Fatal("cross-site login request was accepted")
	}
}

func TestLoginRequestRequiresJSONContentType(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", bytes.NewBufferString(`{"account":"test","password":"test"}`))
	request.Host = "mail.example.com"
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Origin", "https://mail.example.com")
	if loginRequestAllowed(request) {
		t.Fatal("non-JSON login request was accepted")
	}
}

func TestLoginRequestAllowsSameOriginJSON(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", bytes.NewBufferString(`{"account":"test","password":"test"}`))
	request.Host = "mail.example.com"
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Origin", "https://mail.example.com")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if !loginRequestAllowed(request) {
		t.Fatal("same-origin JSON login request was rejected")
	}
}

func TestLoginRequestAllowsNonBrowserJSONClient(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", bytes.NewBufferString(`{"account":"test","password":"test"}`))
	request.Host = "mail.example.com"
	request.Header.Set("Content-Type", "application/json")
	if !loginRequestAllowed(request) {
		t.Fatal("non-browser JSON login client was rejected")
	}
}

func TestCSRFAllowsConfiguredHostOnNonStandardPort(t *testing.T) {
	configureProductionOriginTest(t)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com:8443/api/settings", nil)
	request.Host = "mail.example.com:8443"
	request.Header.Set("Origin", "https://mail.example.com:8443")
	if !sameOriginRequest(request) {
		t.Fatal("configured host on a non-standard port was rejected")
	}
}

func TestSameOriginUsesTrustedForwardedProto(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{
		WebDomain:         "xuans.com",
		TLSMode:           config.HTTPSTLSModeProxy,
		TrustedProxyCIDRs: []string{"127.0.0.1/32"},
	}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})

	request := httptest.NewRequest(http.MethodPost, "http://xuans.com/api/settings", nil)
	request.Host = "xuans.com"
	request.RemoteAddr = "127.0.0.1:15184"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://xuans.com")
	if requestScheme(request) != "https" || !sameOriginRequest(request) {
		t.Fatal("trusted HTTPS proxy request was rejected")
	}
}

func TestSameOriginDoesNotTrustForwardedProtoFromUntrustedClient(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{
		WebDomain:         "xuans.com",
		TLSMode:           config.HTTPSTLSModeProxy,
		TrustedProxyCIDRs: []string{"127.0.0.1/32"},
	}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})

	request := httptest.NewRequest(http.MethodPost, "http://xuans.com/api/settings", nil)
	request.Host = "xuans.com"
	request.RemoteAddr = "192.0.2.10:15184"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://xuans.com")
	if requestScheme(request) != "http" || sameOriginRequest(request) {
		t.Fatal("untrusted client controlled the forwarded protocol")
	}
}

func TestSameOriginRejectsAmbiguousForwardedProto(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{
		WebDomain:         "xuans.com",
		TLSMode:           config.HTTPSTLSModeProxy,
		TrustedProxyCIDRs: []string{"127.0.0.1/32"},
	}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})

	request := httptest.NewRequest(http.MethodPost, "http://xuans.com/api/settings", nil)
	request.Host = "xuans.com"
	request.RemoteAddr = "127.0.0.1:15184"
	request.Header.Set("X-Forwarded-Proto", "https, http")
	request.Header.Set("Origin", "https://xuans.com")
	if requestScheme(request) != "http" || sameOriginRequest(request) {
		t.Fatal("ambiguous forwarded protocol was trusted")
	}
}

func TestHttpsStartSkipsListenerInProxyMode(t *testing.T) {
	oldConfig := config.Instance
	oldManager := config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{
		WebDomain:    "xuans.com",
		TLSMode:      config.HTTPSTLSModeProxy,
		HttpsEnabled: 1,
		HttpsPort:    443,
	}
	httpsServer = nil
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
		httpsServer = nil
	})

	if err := HttpsStart(); err != nil {
		t.Fatalf("proxy mode started HTTPS listener: %v", err)
	}
	if httpsServer != nil {
		t.Fatal("proxy mode created an internal HTTPS server")
	}
}

func TestSecurityHeadersRespectPluginFrameBoundary(t *testing.T) {
	oldConfig := config.Instance
	defer func() { config.Instance = oldConfig }()
	config.Instance = &config.Config{HttpsEnabled: 2}

	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	pluginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pluginRecorder, httptest.NewRequest(http.MethodGet, "/api/plugin/settings/SpamBlock/index.html", nil))
	if pluginRecorder.Header().Get("X-Frame-Options") != "SAMEORIGIN" || pluginRecorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("plugin settings frame boundary is not configured")
	}
	if got := pluginRecorder.Header().Get("Content-Security-Policy"); got == "" || !contains(got, "frame-ancestors 'self'") {
		t.Fatalf("plugin settings CSP is unsafe: %s", got)
	}

	v1PluginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(v1PluginRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/plugin/settings/SpamBlock/index.html", nil))
	if v1PluginRecorder.Header().Get("X-Frame-Options") != "SAMEORIGIN" || !contains(v1PluginRecorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("V1 plugin settings frame boundary is unsafe: headers=%v", v1PluginRecorder.Header())
	}
	pageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pageRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if pageRecorder.Header().Get("X-Frame-Options") != "DENY" || !contains(pageRecorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("ordinary page frame boundary is not configured")
	}
}

func contains(value string, expected string) bool {
	return len(value) >= len(expected) && (value == expected || containsAt(value, expected))
}

func containsAt(value string, expected string) bool {
	for i := 0; i+len(expected) <= len(value); i++ {
		if value[i:i+len(expected)] == expected {
			return true
		}
	}
	return false
}

func TestSecurityHeadersIsolateAttachments(t *testing.T) {
	oldConfig := config.Instance
	defer func() { config.Instance = oldConfig }()
	config.Instance = &config.Config{HttpsEnabled: 2}

	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, path := range []string{"/attachments/1/logo", "/attachments/download/1/0"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		policy := recorder.Header().Get("Content-Security-Policy")
		if recorder.Header().Get("X-Frame-Options") != "DENY" || !contains(policy, "sandbox") || !contains(policy, "default-src 'none'") || !contains(policy, "frame-ancestors 'none'") {
			t.Fatalf("attachment response is not isolated: path=%s policy=%s", path, policy)
		}
	}
}
