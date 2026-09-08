package csrf

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/session"
	"github.com/alexedwards/scs/v2"
)

func testRequestWithToken(t *testing.T, manager *scs.SessionManager, stored, header, cookie string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/settings", nil)
	loaded, err := manager.Load(request.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(loaded)
	if stored != "" {
		manager.Put(request.Context(), SessionKey, stored)
	}
	if header != "" {
		request.Header.Set(HeaderName, header)
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	return request
}

func TestValidRejectsMissingAndMismatchedTokens(t *testing.T) {
	old := session.Instance
	manager := scs.New()
	session.Instance = manager
	t.Cleanup(func() { session.Instance = old })

	cases := []struct {
		name   string
		stored string
		header string
		cookie string
	}{
		{name: "missing-header", stored: "token", cookie: "token"},
		{name: "missing-cookie", stored: "token", header: "token"},
		{name: "header-mismatch", stored: "token", header: "other", cookie: "token"},
		{name: "cookie-mismatch", stored: "token", header: "token", cookie: "other"},
		{name: "missing-session", header: "token", cookie: "token"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if Valid(testRequestWithToken(t, manager, item.stored, item.header, item.cookie)) {
				t.Fatal("invalid CSRF token combination was accepted")
			}
		})
	}
}

func TestClearInvalidatesExistingToken(t *testing.T) {
	old := session.Instance
	manager := scs.New()
	session.Instance = manager
	t.Cleanup(func() { session.Instance = old })

	request := testRequestWithToken(t, manager, "token", "token", "token")
	if !Valid(request) {
		t.Fatal("valid CSRF token was rejected")
	}
	recorder := httptest.NewRecorder()
	Clear(recorder, request, true)
	if Valid(request) {
		t.Fatal("cleared CSRF token remained valid")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName || cookies[0].MaxAge >= 0 || !cookies[0].Secure {
		t.Fatalf("unexpected CSRF clear cookie: %+v", cookies)
	}
}

func TestEnsureRestoresMissingCookieWithRotatedToken(t *testing.T) {
	old := session.Instance
	manager := scs.New()
	session.Instance = manager
	t.Cleanup(func() { session.Instance = old })

	request := testRequestWithToken(t, manager, "old-token", "", "")
	recorder := httptest.NewRecorder()
	token, err := Ensure(recorder, request, true)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token == "old-token" {
		t.Fatal("missing cookie did not rotate the CSRF token")
	}
	if stored := manager.GetString(request.Context(), SessionKey); stored != token {
		t.Fatalf("rotated token was not stored in session: %q", stored)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("unexpected CSRF cookies: %+v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != CookieName || cookie.Value != token || cookie.Path != "/" ||
		cookie.MaxAge != 86400 || !cookie.Secure || cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected restored CSRF cookie: %+v", cookie)
	}
}

func TestEnsureKeepsMatchingTokenWithoutReissuingCookie(t *testing.T) {
	old := session.Instance
	manager := scs.New()
	session.Instance = manager
	t.Cleanup(func() { session.Instance = old })

	request := testRequestWithToken(t, manager, "current-token", "", "current-token")
	recorder := httptest.NewRecorder()
	token, err := Ensure(recorder, request, false)
	if err != nil {
		t.Fatal(err)
	}
	if token != "current-token" {
		t.Fatalf("matching token was unexpectedly rotated: %q", token)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("matching token unexpectedly reissued cookies: %+v", cookies)
	}
}

func TestEnsureRotatesMismatchedCookie(t *testing.T) {
	old := session.Instance
	manager := scs.New()
	session.Instance = manager
	t.Cleanup(func() { session.Instance = old })

	request := testRequestWithToken(t, manager, "stored-token", "", "stale-token")
	recorder := httptest.NewRecorder()
	token, err := Ensure(recorder, request, false)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token == "stored-token" || token == "stale-token" {
		t.Fatalf("mismatched cookie did not rotate token: %q", token)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("unexpected development CSRF cookie: %+v", cookies)
	}
}
