package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicRecoveryFlowHTTP(t *testing.T) {
	enablePublicRecoveryForTest(t)
	s, _, _, user, super := newPublicSecurityFixture(t)
	previous := currentSecurityRuntime()
	ConfigureSecurityRuntime(nil, nil, s, nil)
	oldBegin, oldFailure := emailRecoveryBeginIPAttempts, emailRecoveryFailureIPAttempts
	emailRecoveryBeginIPAttempts, emailRecoveryFailureIPAttempts = newLoginLimiter(100), newLoginLimiter(100)
	t.Cleanup(func() {
		ConfigureSecurityRuntime(previous.verification, previous.external, previous.recovery, previous.credentials)
		emailRecoveryBeginIPAttempts, emailRecoveryFailureIPAttempts = oldBegin, oldFailure
	})
	invoke := func(endpoint, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "https://example.com/rest/api/recovery/"+endpoint, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.70:12345"
		w := httptest.NewRecorder()
		PublicPasswordRecoveryFlow(w, r)
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("recovery response cached")
		}
		return w.Code, data
	}
	status, data := invoke("begin", `{"identifier":"`+super.Account+`"}`)
	if status != 200 || data["role"] != "super_admin" || data["token"] != nil {
		t.Fatalf("super response: %v", data)
	}
	status, data = invoke("begin", `{"identifier":"`+user.Account+`"}`)
	if status != 200 || data["email"] != "a***@example.com" {
		t.Fatalf("identity response: %v", data)
	}
	token := data["token"].(string)
	if status, _ := invoke("reset", `{"token":"`+token+`","new_password":"NewPass123"}`); status == 200 {
		t.Fatal("unverified reset accepted")
	}
	if status, _ := invoke("verify", `{"token":"`+token+`","method":"email","code":"12345678"}`); status == 200 {
		t.Fatal("unsent code accepted")
	}
	if status, _ := invoke("begin", `{"identifier":"missing"}`); status == 200 {
		t.Fatal("missing user accepted")
	}
	if status, _ := invoke("begin", `{"identifier":"alice","role":"super_admin"}`); status == 200 {
		t.Fatal("client role accepted")
	}
}
