package http_server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func withSetupSecurity(t *testing.T, token string) *setupSecurity {
	t.Helper()
	oldToken := os.Getenv("SMAIL_SETUP_TOKEN")
	oldCIDRs := os.Getenv("SMAIL_SETUP_ALLOWED_CIDRS")
	oldInit := config.IsInit
	if err := os.Setenv("SMAIL_SETUP_TOKEN", token); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("SMAIL_SETUP_ALLOWED_CIDRS"); err != nil {
		t.Fatal(err)
	}
	config.IsInit = false
	t.Cleanup(func() {
		config.IsInit = oldInit
		_ = os.Setenv("SMAIL_SETUP_TOKEN", oldToken)
		_ = os.Setenv("SMAIL_SETUP_ALLOWED_CIDRS", oldCIDRs)
	})
	security, err := newSetupSecurity()
	if err != nil {
		t.Fatal(err)
	}
	return security
}

func setupRequest(method string) *http.Request {
	request := httptest.NewRequest(method, "http://127.0.0.1/api/setup", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	return request
}

func TestSetupSecurityRejectsMissingAndInvalidTokens(t *testing.T) {
	security := withSetupSecurity(t, "0123456789abcdefghijklmnop")
	handler := security.authorize(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, token := range []string{"", "invalid-token-value-123456789"} {
		request := setupRequest(http.MethodPost)
		if token != "" {
			request.Header.Set("X-Setup-Token", token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("token %q returned %d", token, recorder.Code)
		}
	}
}

func TestSetupSecurityAcceptsTokenOnce(t *testing.T) {
	token := "0123456789abcdefghijklmnop"
	security := withSetupSecurity(t, token)
	calls := 0
	handler := security.authorize(func(w http.ResponseWriter, r *http.Request) {
		calls++
		config.IsInit = true
		w.WriteHeader(http.StatusNoContent)
	})

	request := setupRequest(http.MethodPost)
	request.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("first request failed: code=%d calls=%d", recorder.Code, calls)
	}

	config.IsInit = false
	request = setupRequest(http.MethodPost)
	request.Header.Set("X-Setup-Token", token)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || calls != 1 {
		t.Fatalf("replay was accepted: code=%d calls=%d", recorder.Code, calls)
	}
}

func TestSetupSecurityBlocksUnauthorizedSource(t *testing.T) {
	token := "0123456789abcdefghijklmnop"
	security := withSetupSecurity(t, token)
	handler := security.authorize(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := setupRequest(http.MethodPost)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unauthorized source returned %d", recorder.Code)
	}
}

func TestSetupSecurityRateLimit(t *testing.T) {
	security := withSetupSecurity(t, "0123456789abcdefghijklmnop")
	handler := security.authorize(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, setupRequest(http.MethodPost))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("attempt %d returned %d", i+1, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, setupRequest(http.MethodPost))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked request returned %d", recorder.Code)
	}
}

func TestSetupSecurityBootstrapCookie(t *testing.T) {
	security := withSetupSecurity(t, "0123456789abcdefghijklmnop")
	handler := security.bootstrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("bootstrap returned %d", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != setupCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/setup" {
		t.Fatalf("unsafe setup cookie: %+v", cookies)
	}
}
