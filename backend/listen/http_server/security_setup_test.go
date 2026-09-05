package http_server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/session"
	"github.com/alexedwards/scs/v2"
)

func TestSecuritySetupRouteRequiresPostAndCSRF(t *testing.T) {
	configureProductionOriginTest(t)
	previous := session.Instance
	session.Instance = scs.New()
	session.Instance.Cookie.Name = "smail_session"
	t.Cleanup(func() { session.Instance = previous })
	mux := http.NewServeMux()
	registerV1Routes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/security/setup/acknowledge", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatal("GET acknowledgement was accepted")
	}
	for _, origin := range []string{"https://evil.example.com", "https://mail.example.com"} {
		request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/v1/security/setup/acknowledge", nil)
		request.Header.Set("Origin", origin)
		request.AddCookie(&http.Cookie{Name: "smail_session", Value: "test"})
		recorder = httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.ErrorNo != response.ParamsError {
			t.Fatal("request without valid CSRF was accepted")
		}
	}
}
