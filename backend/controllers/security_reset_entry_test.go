package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestPublicRecoveryEnabledRequiresReadyStateAndConfiguredHost(t *testing.T) {
	old := config.Instance
	oldManager := config.CurrentManager()
	t.Cleanup(func() {
		config.Instance = old
		config.SetRuntimeManager(oldManager)
	})

	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "disabled", cfg: &config.Config{SetupState: config.SetupStateReady, WebDomain: "mail.example.com"}, want: false},
		{name: "not_ready", cfg: &config.Config{ExternalRecoveryEnabled: true, WebDomain: "mail.example.com"}, want: false},
		{name: "missing_host", cfg: &config.Config{ExternalRecoveryEnabled: true, SetupState: config.SetupStateReady}, want: false},
		{name: "enabled", cfg: &config.Config{ExternalRecoveryEnabled: true, SetupState: config.SetupStateReady, WebDomain: "mail.example.com"}, want: true},
		{name: "ip_host", cfg: &config.Config{ExternalRecoveryEnabled: true, SetupState: config.SetupStateReady, WebDomain: "192.0.2.10:8443"}, want: true},
		{name: "configured_ip", cfg: &config.Config{ExternalRecoveryEnabled: true, SetupState: config.SetupStateReady, AccessHosts: []string{"192.0.2.11"}}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config.SetRuntimeManager(nil)
			config.Instance = test.cfg
			if got := PublicRecoveryEnabled(); got != test.want {
				t.Fatalf("PublicRecoveryEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRecoveryRejectsWhenDisabled(t *testing.T) {
	old := config.Instance
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{SetupState: config.SetupStateReady, WebDomain: "mail.example.com"}
	t.Cleanup(func() { config.Instance = old })

	recorder := httptest.NewRecorder()
	BeginPasswordRecovery(&appcontext.Context{}, recorder, httptest.NewRequest(http.MethodPost, "/api/security/recovery/begin", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected response status: %d", recorder.Code)
	}
	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorNo == 0 {
		t.Fatal("disabled recovery request was accepted")
	}
}

func TestRecoveryBrowserContextCookieProperties(t *testing.T) {
	oldDevelopment := config.DevelopmentMode
	config.DevelopmentMode = true
	t.Cleanup(func() { config.DevelopmentMode = oldDevelopment })
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/security/recovery/begin", nil)
	recorder := httptest.NewRecorder()
	value, ok := recoveryBrowserContext(request, recorder, true)
	if !ok || value == "" {
		t.Fatal("recovery context was not created")
	}
	cookie := recorder.Result().Cookies()[0]
	if cookie.Name != recoveryContextCookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/" || cookie.Secure {
		t.Fatalf("unexpected recovery context cookie: %+v", cookie)
	}
	request.AddCookie(cookie)
	second := httptest.NewRecorder()
	if got, ok := recoveryBrowserContext(request, second, false); !ok || got != value {
		t.Fatal("existing recovery context was not reused")
	}
	if second.Header().Get("Set-Cookie") != "" {
		t.Fatal("existing recovery context was unexpectedly replaced")
	}
}

func TestRecoveryBrowserContextCookieIsRequiredForCompletion(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/security/recovery/complete", nil)
	recorder := httptest.NewRecorder()
	if value, ok := recoveryBrowserContext(request, recorder, false); ok || value != "" {
		t.Fatal("missing recovery context was accepted")
	}
}
