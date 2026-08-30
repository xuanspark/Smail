package http_server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestNormalizedAPIPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/login", want: "/api/login"},
		{path: "/api/v1/login", want: "/api/login"},
		{path: "/api/v1/security/recovery/begin", want: "/api/security/recovery/begin"},
		{path: "/api/v1", want: "/api"},
	}
	for _, test := range tests {
		if got := normalizedAPIPath(test.path); got != test.want {
			t.Fatalf("normalizedAPIPath(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestV1RouteRewritesControllerPath(t *testing.T) {
	var got string
	handler := rewriteV1PathPrefix("/api/plugin/settings/", func(_ *appcontext.Context, w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plugin/settings/TestPlugin/index.html", nil)
	recorder := httptest.NewRecorder()
	handler(&appcontext.Context{}, recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("rewritten handler returned %d", recorder.Code)
	}
	if got != "/api/plugin/settings/TestPlugin/index.html" {
		t.Fatalf("controller path = %q", got)
	}
	if request.URL.Path != "/api/v1/plugin/settings/TestPlugin/index.html" {
		t.Fatalf("request path was not restored: %q", request.URL.Path)
	}
}

func TestV1PostOnlyRoutesRejectGet(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		"/api/v1/email/detail",
		"/api/v1/email/list",
		"/api/v1/user/info",
		"/api/v1/user/list",
		"/api/v1/rule/get",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s returned %d, want %d", endpoint, recorder.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestLegacyRouteIncludesDeprecationHeaders(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/info", nil))
	if recorder.Header().Get("Deprecation") != "true" {
		t.Fatalf("legacy route missing Deprecation header: %v", recorder.Header())
	}
	if recorder.Header().Get("X-Smail-API-Version") != "legacy" {
		t.Fatalf("legacy route missing version header: %v", recorder.Header())
	}
	if recorder.Header().Get("Link") == "" {
		t.Fatalf("legacy route missing successor link: %v", recorder.Header())
	}
}

func TestV1RouteDoesNotReceiveLegacyDeprecationHeaders(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/user/info", nil))
	if recorder.Header().Get("Deprecation") != "" {
		t.Fatalf("V1 route unexpectedly marked deprecated: %v", recorder.Header())
	}
}

func TestV1LoginUsesSameRequestOriginGuard(t *testing.T) {
	oldInit := config.IsInit
	t.Cleanup(func() { config.SetInitState(oldInit) })
	config.SetInitState(false)

	handler := contextIterceptor(controllers.Login)
	request := httptest.NewRequest(http.MethodPost, "http://mail.example.com/api/v1/login", nil)
	request.Host = "mail.example.com"
	request.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("V1 login guard returned HTTP %d", recorder.Code)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("V1 login guard returned an empty response")
	}
}

func TestLegacyCredentialsQuestionMethodErrorIncludesDeprecationHeaders(t *testing.T) {
	legacy := credentialsQuestionHandler()
	legacyRecorder := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(http.MethodDelete, "/api/security/credentials/question", nil)
	legacy.ServeHTTP(legacyRecorder, legacyRequest)
	if legacyRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy method returned %d, want %d", legacyRecorder.Code, http.StatusMethodNotAllowed)
	}
	if legacyRecorder.Header().Get("Deprecation") != "true" {
		t.Fatalf("legacy method error missing Deprecation header: %v", legacyRecorder.Header())
	}

	mux := http.NewServeMux()
	registerV1CredentialsQuestionRoute(mux)
	v1Recorder := httptest.NewRecorder()
	v1Request := httptest.NewRequest(http.MethodDelete, "/api/v1/security/credentials/question", nil)
	mux.ServeHTTP(v1Recorder, v1Request)
	if v1Recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("V1 method returned %d, want %d", v1Recorder.Code, http.StatusMethodNotAllowed)
	}
	if v1Recorder.Header().Get("Deprecation") != "" {
		t.Fatalf("V1 method error unexpectedly marked deprecated: %v", v1Recorder.Header())
	}
}

func TestFrontendV1RoutesAreRegistered(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		"/api/v1/audits/config",
		"/api/v1/audits/config/export",
		"/api/v1/audits/config/audit-00000000000000000000000000000000",
		"/api/v1/database/migrations/preview",
		"/api/v1/database/migrations/dbmig-00000000000000000000000000000000",
		"/api/v1/system/restart/preview",
		"/api/v1/config/status",
		"/api/v1/config/backups",
		"/api/v1/config/rollback",
		"/api/v1/config/rollback/preview",
		"/api/v1/config/rollback/ticket",
		"/api/v1/config/mail",
		"/api/v1/config/mail/validate",
		"/api/v1/config/mail/apply",
		"/api/v1/email/del",
		"/api/v1/email/detail",
		"/api/v1/email/list",
		"/api/v1/email/move",
		"/api/v1/email/read",
		"/api/v1/email/send",
		"/api/v1/group",
		"/api/v1/group/add",
		"/api/v1/group/del",
		"/api/v1/group/list",
		"/api/v1/login",
		"/api/v1/logout",
		"/api/v1/plugin/list",
		"/api/v1/plugin/settings/example/index.html",
		"/api/v1/plugin/settings/telegram_push/user",
		"/api/v1/plugin/settings/telegram_push/bot",
		"/api/v1/plugin/settings/telegram_push/test",
		"/api/v1/rule/add",
		"/api/v1/rule/del",
		"/api/v1/rule/get",
		"/api/v1/rule/update",
		"/api/v1/security/credentials/question",
		"/api/v1/security/credentials/recovery-codes",
		"/api/v1/security/credentials/recovery-codes/generate",
		"/api/v1/security/credentials/recovery-codes/revoke",
		"/api/v1/security/external-email/bind",
		"/api/v1/security/external-email/disable",
		"/api/v1/security/external-email/list",
		"/api/v1/security/external-email/verify",
		"/api/v1/security/login/resend",
		"/api/v1/security/login/verify",
		"/api/v1/security/recovery/begin",
		"/api/v1/security/recovery/complete",
		"/api/v1/security/recovery/verify",
		"/api/v1/security/recovery/webauthn/begin",
		"/api/v1/security/recovery/webauthn/verify",
		"/api/v1/security/verification/request",
		"/api/v1/security/verification/verify",
		"/api/v1/security/webauthn/credentials",
		"/api/v1/security/webauthn/credentials/rename",
		"/api/v1/security/webauthn/credentials/revoke",
		"/api/v1/security/webauthn/login/begin",
		"/api/v1/security/webauthn/login/finish",
		"/api/v1/security/webauthn/registration/begin",
		"/api/v1/security/webauthn/registration/finish",
		"/api/v1/settings/modify_password",
		"/api/v1/system/restart",
		"/api/v1/user/create",
		"/api/v1/user/edit",
		"/api/v1/user/info",
		"/api/v1/user/list",
	}
	for _, path := range paths {
		handler, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, path, nil))
		if handler == nil || pattern == "" || pattern == "/" {
			t.Errorf("V1 route %s fell through to pattern %q", path, pattern)
		}
	}
}
