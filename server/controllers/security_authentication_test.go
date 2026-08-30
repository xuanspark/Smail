package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestSecurityHandlersRejectUnauthenticatedRequests(t *testing.T) {
	tests := []struct {
		name string
		hand func(*appcontext.Context, http.ResponseWriter, *http.Request)
		body string
	}{
		{name: "get security question", hand: GetSecurityQuestion},
		{name: "set security question", hand: SetSecurityQuestion, body: `{"question":"问题","answer":"答案"}`},
		{name: "generate recovery codes", hand: GenerateRecoveryCodes, body: `{"count":5}`},
		{name: "get recovery code status", hand: GetRecoveryCodeStatus},
		{name: "revoke recovery codes", hand: RevokeRecoveryCodes},
		{name: "list protocol credentials", hand: ListProtocolCredentials},
		{name: "set protocol enabled", hand: SetProtocolCredentialEnabled, body: `{"protocol":"smtp","enabled":true}`},
		{name: "generate protocol credential", hand: GenerateProtocolCredential, body: `{"protocol":"smtp"}`},
		{name: "revoke protocol credential", hand: RevokeProtocolCredential, body: `{"protocol":"smtp"}`},
		{name: "request verification", hand: RequestVerification, body: `{"purpose":"login","account":"smail","email":"user@example.com"}`},
		{name: "verify verification", hand: VerifyVerification, body: `{"id":"challenge","account":"smail","email":"user@example.com","code":"12345678","purpose":"login"}`},
		{name: "list external emails", hand: ListExternalEmails},
		{name: "bind external email", hand: BindExternalEmail, body: `{"email":"backup@example.com","type":"backup"}`},
		{name: "verify external email", hand: VerifyExternalEmail, body: `{"id":"challenge","binding_id":1,"email":"backup@example.com","code":"12345678"}`},
		{name: "disable external email", hand: DisableExternalEmail, body: `{"id":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			test.hand(&appcontext.Context{}, recorder, request)

			var result response.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatalf("invalid JSON response: %v", err)
			}
			if result.ErrorNo != response.NeedLogin {
				t.Fatalf("error number = %d, want %d; body=%s", result.ErrorNo, response.NeedLogin, recorder.Body.String())
			}
		})
	}
}

func TestSuperAdminCannotManageRecoveryCodesThroughWeb(t *testing.T) {
	ctx := &appcontext.Context{UserID: 1, Role: access.RoleSuperAdmin}
	request := httptest.NewRequest(http.MethodPost, "/api/security/credentials/recovery-codes", strings.NewReader(`{"count":5}`))
	recorder := httptest.NewRecorder()
	GenerateRecoveryCodes(ctx, recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if result.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("error number = %d, want %d; body=%s", result.ErrorNo, response.NoAccessPrivileges, recorder.Body.String())
	}
}

func TestProtocolCredentialHandlersManageCurrentUserCredential(t *testing.T) {
	_, _, engine, user, _ := newPublicSecurityFixture(t)
	credentialService, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	previous := securityRuntime
	securityRuntime.credentials = credentialService
	t.Cleanup(func() { securityRuntime = previous })

	ctx := &appcontext.Context{UserID: user.ID, UserAccount: user.Account}
	list := func() response.Response {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/security/credentials/protocols", nil)
		ListProtocolCredentials(ctx, recorder, request)
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("invalid protocol list response: %v; body=%s", err, recorder.Body.String())
		}
		return result
	}

	initial := list()
	if initial.ErrorNo != 0 {
		t.Fatalf("list protocol credentials failed: %+v", initial)
	}

	setRecorder := httptest.NewRecorder()
	setRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/security/credentials/protocols/enabled",
		strings.NewReader(`{"protocol":"SMTP","enabled":true}`),
	)
	SetProtocolCredentialEnabled(ctx, setRecorder, setRequest)
	var setResult response.Response
	if err := json.Unmarshal(setRecorder.Body.Bytes(), &setResult); err != nil {
		t.Fatalf("invalid protocol enable response: %v", err)
	}
	if setResult.ErrorNo != 0 {
		t.Fatalf("enable protocol failed: %+v", setResult)
	}

	generateRecorder := httptest.NewRecorder()
	generateRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/security/credentials/protocols/generate",
		strings.NewReader(`{"protocol":"smtp"}`),
	)
	GenerateProtocolCredential(ctx, generateRecorder, generateRequest)
	var generateResult response.Response
	if err := json.Unmarshal(generateRecorder.Body.Bytes(), &generateResult); err != nil {
		t.Fatalf("invalid protocol generate response: %v", err)
	}
	if generateResult.ErrorNo != 0 {
		t.Fatalf("generate protocol credential failed: %+v", generateResult)
	}
	generated, ok := generateResult.Data.(map[string]any)
	if !ok || generated["code"] == "" || generated["shown_once"] != true {
		t.Fatalf("unexpected generated protocol response: %#v", generateResult.Data)
	}
	if strings.Contains(generateRecorder.Body.String(), "code_hash") {
		t.Fatal("protocol response exposed code hash")
	}

	status := list()
	statusData, ok := status.Data.([]any)
	if !ok || len(statusData) != 3 {
		t.Fatalf("unexpected protocol status response: %#v", status.Data)
	}
	foundSMTP := false
	for _, item := range statusData {
		itemMap, ok := item.(map[string]any)
		if !ok || itemMap["protocol"] != "smtp" {
			continue
		}
		foundSMTP = true
		if itemMap["enabled"] != true || itemMap["configured"] != true {
			t.Fatalf("unexpected SMTP status: %#v", itemMap)
		}
	}
	if !foundSMTP {
		t.Fatal("SMTP protocol status was not returned")
	}

	revokeRecorder := httptest.NewRecorder()
	revokeRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/security/credentials/protocols/revoke",
		strings.NewReader(`{"protocol":"smtp"}`),
	)
	RevokeProtocolCredential(ctx, revokeRecorder, revokeRequest)
	var revokeResult response.Response
	if err := json.Unmarshal(revokeRecorder.Body.Bytes(), &revokeResult); err != nil {
		t.Fatalf("invalid protocol revoke response: %v", err)
	}
	if revokeResult.ErrorNo != 0 {
		t.Fatalf("revoke protocol credential failed: %+v", revokeResult)
	}
	status = list()
	statusData, ok = status.Data.([]any)
	if !ok || len(statusData) != 3 {
		t.Fatalf("unexpected revoked protocol status response: %#v", status.Data)
	}
	for _, item := range statusData {
		itemMap, ok := item.(map[string]any)
		if ok && itemMap["protocol"] == "smtp" && itemMap["configured"] == true {
			t.Fatal("revoked SMTP protocol credential remained configured")
		}
	}
}
