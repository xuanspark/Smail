package http_server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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
	config.SetInitState(false)
	t.Cleanup(func() {
		config.SetInitState(oldInit)
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
		config.SetInitState(true)
		w.WriteHeader(http.StatusNoContent)
	})

	request := setupRequest(http.MethodPost)
	request.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("first request failed: code=%d calls=%d", recorder.Code, calls)
	}

	config.SetInitState(false)
	request = setupRequest(http.MethodPost)
	request.Header.Set("X-Setup-Token", token)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || calls != 1 {
		t.Fatalf("replay was accepted: code=%d calls=%d", recorder.Code, calls)
	}
}

func TestSetupSecuritySerializesConcurrentTokenRequests(t *testing.T) {
	token := "0123456789abcdefghijklmnop"
	security := withSetupSecurity(t, token)
	started := make(chan struct{})
	continueRequest := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	handler := security.authorize(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		close(started)
		<-continueRequest
		config.SetInitState(true)
		w.WriteHeader(http.StatusNoContent)
	})

	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			request := setupRequest(http.MethodPost)
			request.Header.Set("X-Setup-Token", token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			results <- recorder.Code
		}()
	}

	<-started
	close(continueRequest)
	first, second := <-results, <-results
	if (first != http.StatusNoContent && second != http.StatusNoContent) || (first == http.StatusNoContent && second == http.StatusNoContent) {
		t.Fatalf("concurrent setup results = %d, %d; want exactly one success", first, second)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Fatalf("setup handler calls = %d, want 1", calls)
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
	if len(cookies) != 1 || cookies[0].Name != setupCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/" {
		t.Fatalf("unsafe setup cookie: %+v", cookies)
	}
}

func TestSetupAPIRoutesUseEquivalentAuthorization(t *testing.T) {
	const token = "0123456789abcdefghijklmnop"
	for _, endpoint := range []string{"/api/setup", "/api/v1/setup"} {
		t.Run(endpoint, func(t *testing.T) {
			security := withSetupSecurity(t, token)
			mux := http.NewServeMux()
			calls := 0
			registerSetupAPIRoutes(mux, security, setupOnlyPost(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(http.MethodPost, endpoint, nil)
			request.RemoteAddr = "127.0.0.1:12345"
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || calls != 0 {
				t.Fatalf("missing token returned code=%d calls=%d", recorder.Code, calls)
			}

			request = httptest.NewRequest(http.MethodPost, endpoint, nil)
			request.RemoteAddr = "127.0.0.1:12345"
			request.Header.Set("X-Setup-Token", token)
			recorder = httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent || calls != 1 {
				t.Fatalf("valid token returned code=%d calls=%d", recorder.Code, calls)
			}
		})
	}
}

func TestSetupAPIRoutesRequirePostAndToken(t *testing.T) {
	const token = "0123456789abcdefghijklmnop"
	for _, endpoint := range []string{"/api/setup", "/api/v1/setup"} {
		t.Run(endpoint, func(t *testing.T) {
			security := withSetupSecurity(t, token)
			mux := http.NewServeMux()
			calls := 0
			registerSetupAPIRoutes(mux, security, setupOnlyPost(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(http.MethodGet, endpoint, nil)
			request.RemoteAddr = "127.0.0.1:12345"
			request.Header.Set("X-Setup-Token", token)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost || calls != 0 {
				t.Fatalf("GET returned code=%d allow=%q calls=%d", recorder.Code, recorder.Header().Get("Allow"), calls)
			}
		})
	}
}

func TestSetupAPIRoutesBecomeInvalidAfterSetupCompletes(t *testing.T) {
	const token = "0123456789abcdefghijklmnop"
	security := withSetupSecurity(t, token)
	mux := http.NewServeMux()
	calls := 0
	registerSetupAPIRoutes(mux, security, setupOnlyPost(func(w http.ResponseWriter, r *http.Request) {
		calls++
		config.SetInitState(true)
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/setup", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("first setup returned code=%d calls=%d", recorder.Code, calls)
	}

	config.SetInitState(false)
	for _, endpoint := range []string{"/api/setup", "/api/v1/setup"} {
		request = httptest.NewRequest(http.MethodPost, endpoint, nil)
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("X-Setup-Token", token)
		recorder = httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || calls != 1 {
			t.Fatalf("replayed setup %s returned code=%d calls=%d", endpoint, recorder.Code, calls)
		}
	}
}

func TestSetupSecurityBootstrapCookieForAllowedRemote(t *testing.T) {
	const token = "0123456789abcdefghijklmnop"
	oldToken := os.Getenv("SMAIL_SETUP_TOKEN")
	oldCIDRs := os.Getenv("SMAIL_SETUP_ALLOWED_CIDRS")
	oldInit := config.IsInit
	if err := os.Setenv("SMAIL_SETUP_TOKEN", token); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("SMAIL_SETUP_ALLOWED_CIDRS", "192.168.2.0/24"); err != nil {
		t.Fatal(err)
	}
	config.SetInitState(false)
	t.Cleanup(func() {
		config.SetInitState(oldInit)
		_ = os.Setenv("SMAIL_SETUP_TOKEN", oldToken)
		_ = os.Setenv("SMAIL_SETUP_ALLOWED_CIDRS", oldCIDRs)
	})

	security, err := newSetupSecurity()
	if err != nil {
		t.Fatal(err)
	}
	handler := security.bootstrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://192.168.2.3/", nil)
	request.RemoteAddr = "192.168.2.2:12345"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("remote bootstrap returned %d", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != setupCookieName || cookies[0].Value != token || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/" {
		t.Fatalf("unsafe remote setup cookie: %+v", cookies)
	}
}
