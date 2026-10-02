package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
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
		{name: "reveal protocol credentials", hand: RevealProtocolCredentials, body: `{"password":"FixtureP12"}`},
		{name: "list client credentials", hand: ListClientCredentials},
		{name: "create client credential", hand: CreateClientCredential, body: `{"client_name":"设备","password":"FixtureP12"}`},
		{name: "regenerate client credential", hand: RegenerateClientCredential, body: `{"client_id":1,"password":"FixtureP12"}`},
		{name: "revoke client credential", hand: RevokeClientCredential, body: `{"client_id":1,"password":"FixtureP12"}`},
		{name: "reveal client credentials", hand: RevealClientCredentials, body: `{"password":"FixtureP12"}`},
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

func TestSuperAdminRecoveryCodesRequireRecentAuthentication(t *testing.T) {
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

func TestBindExternalEmailReturnsStableRecentAuthenticationCode(t *testing.T) {
	previous := securityRuntime
	securityRuntime.external = &externalemail.Repository{}
	securityRuntime.verification = &verification.Service{}
	t.Cleanup(func() { securityRuntime = previous })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/security/external-email/bind",
		strings.NewReader(`{"email":"backup@example.com","type":"backup"}`),
	)
	BindExternalEmail(&appcontext.Context{UserID: 1, UserAccount: "alice"}, recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if result.ErrorNo != response.NoAccessPrivileges || !ok || data["code"] != string(access.CodeRecentAuthRequired) {
		t.Fatalf("recent authentication response = %+v", result)
	}
}

func TestChangeOwnExternalEmailRequiresRecentAuthentication(t *testing.T) {
	previous := securityRuntime
	securityRuntime.external = &externalemail.Repository{}
	securityRuntime.verification = &verification.Service{}
	t.Cleanup(func() { securityRuntime = previous })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/security/external-email/change",
		strings.NewReader(`{"email":"backup@example.com","type":"primary"}`),
	)
	ChangeOwnExternalEmail(&appcontext.Context{UserID: 1, UserAccount: "alice"}, recorder, request)

	var result response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if result.ErrorNo != response.NoAccessPrivileges || !ok || data["code"] != string(access.CodeRecentAuthRequired) {
		t.Fatalf("recent authentication response = %+v", result)
	}
}

func TestProtocolCredentialHandlersManageCurrentUserCredential(t *testing.T) {
	_, _, engine, user, _ := newPublicSecurityFixture(t)
	credentialService, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	tempVault := t.TempDir()
	credentialService.SetVaultRoot(tempVault)
	previousDB := db.Instance
	db.Instance = engine
	previous := securityRuntime
	securityRuntime.credentials = credentialService
	t.Cleanup(func() {
		securityRuntime = previous
		db.Instance = previousDB
	})

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
		strings.NewReader(`{"protocol":"SMTP","enabled":true,"password":"FixtureP12"}`),
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
		strings.NewReader(`{"protocol":"smtp","password":"FixtureP12"}`),
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

	revealFailRecorder := httptest.NewRecorder()
	revealFailRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/security/credentials/protocols/reveal",
		strings.NewReader(`{"password":"wrong-password"}`),
	)
	RevealProtocolCredentials(ctx, revealFailRecorder, revealFailRequest)
	var revealFailResult response.Response
	if err := json.Unmarshal(revealFailRecorder.Body.Bytes(), &revealFailResult); err != nil {
		t.Fatalf("invalid reveal fail response: %v", err)
	}
	if revealFailResult.ErrorNo == 0 {
		t.Fatal("expected wrong password to fail reveal, got errorNo 0")
	}

	revealSuccessRecorder := httptest.NewRecorder()
	revealSuccessRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/security/credentials/protocols/reveal",
		strings.NewReader(`{"password":"FixtureP12"}`),
	)
	RevealProtocolCredentials(ctx, revealSuccessRecorder, revealSuccessRequest)
	var revealSuccessResult response.Response
	if err := json.Unmarshal(revealSuccessRecorder.Body.Bytes(), &revealSuccessResult); err != nil {
		t.Fatalf("invalid reveal success response: %v", err)
	}
	if revealSuccessResult.ErrorNo != 0 {
		t.Fatalf("reveal protocol credentials failed: %+v", revealSuccessResult)
	}
	revealedMap, ok := revealSuccessResult.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any reveal response, got %#v", revealSuccessResult.Data)
	}
	if revealedMap["smtp"] != generated["code"] {
		t.Fatalf("expected revealed smtp code %v, got %v", generated["code"], revealedMap["smtp"])
	}

	revokeRecorder := httptest.NewRecorder()
	revokeRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/security/credentials/protocols/revoke",
		strings.NewReader(`{"protocol":"smtp","password":"FixtureP12"}`),
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

func TestClientCredentialEntryPointsRequireOwnPassword(t *testing.T) {
	_, _, engine, user, other := newPublicSecurityFixture(t)
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	service.SetVaultRoot(t.TempDir())
	previousDB, previousRuntime := db.Instance, securityRuntime
	previousUserLimiter, previousIPLimiter := reauthUserAttempts, reauthIPAttempts
	db.Instance, securityRuntime.credentials = engine, service
	t.Cleanup(func() {
		db.Instance, securityRuntime = previousDB, previousRuntime
		reauthUserAttempts, reauthIPAttempts = previousUserLimiter, previousIPLimiter
	})
	_, client, err := service.CreateClientCredential(user.ID, "原设备")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetProtocolEnabled(user.ID, "smtp", true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GenerateProtocolCode(user.ID, "smtp"); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		t.Helper()
		clients, err := service.ListClientCredentials(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		protocols, err := service.ListProtocolCredentials(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal([]any{clients, protocols})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before := status()
	entries := []struct {
		name string
		hand func(*appcontext.Context, http.ResponseWriter, *http.Request)
		body string
	}{
		{"create", CreateClientCredential, `{"client_name":"新设备"`},
		{"regenerate", RegenerateClientCredential, fmt.Sprintf(`{"client_id":%d`, client.ID)},
		{"revoke", RevokeClientCredential, fmt.Sprintf(`{"client_id":%d`, client.ID)},
		{"reveal", RevealClientCredentials, `{`},
		{"enable protocol", SetProtocolCredentialEnabled, `{"protocol":"imap","enabled":true`},
		{"disable protocol", SetProtocolCredentialEnabled, `{"protocol":"smtp","enabled":false`},
		{"generate protocol", GenerateProtocolCredential, `{"protocol":"smtp"`},
		{"revoke protocol", RevokeProtocolCredential, `{"protocol":"smtp"`},
		{"reveal protocol", RevealProtocolCredentials, `{`},
	}
	for _, role := range []access.Role{access.RoleUser, access.RoleAdmin, access.RoleSuperAdmin} {
		for _, entry := range entries {
			t.Run(string(role)+"/"+entry.name, func(t *testing.T) {
				reauthUserAttempts, reauthIPAttempts = newLoginLimiter(10), newLoginLimiter(50)
				ctx := &appcontext.Context{UserID: user.ID, Role: role, RecentAuth: true}
				expectedFailures := 0
				for _, suffix := range []string{`}`, `,"password":"incorrect"}`, `,"password":" FixtureP12 "}`} {
					if entry.body == "{" {
						suffix = strings.TrimPrefix(suffix, ",")
					}
					recorder := httptest.NewRecorder()
					entry.hand(ctx, recorder, httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(entry.body+suffix)))
					var result response.Response
					if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.ErrorNo == 0 {
						t.Fatal("missing or incorrect password must not allow credential operations")
					}
					if suffix != "}" {
						expectedFailures++
						key := reauthUserAttempts.accountKey(strconv.Itoa(user.ID))
						reauthUserAttempts.mu.Lock()
						failures := reauthUserAttempts.attempts[key].failures
						reauthUserAttempts.mu.Unlock()
						if failures != expectedFailures {
							t.Fatal("incorrect password must reach authentication and record a failed attempt")
						}
					}
					if recorder.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("credential responses must not be cached")
					}
					if status() != before {
						t.Fatal("rejected operation changed credential state")
					}
				}
			})
		}
	}
	// 本人验证通过也不能重置或撤销其他账号的客户端。
	reauthUserAttempts, reauthIPAttempts = newLoginLimiter(10), newLoginLimiter(50)
	for _, hand := range []func(*appcontext.Context, http.ResponseWriter, *http.Request){RegenerateClientCredential, RevokeClientCredential} {
		recorder := httptest.NewRecorder()
		body := fmt.Sprintf(`{"client_id":%d,"password":"FixtureP12"}`, client.ID)
		hand(&appcontext.Context{UserID: other.ID, Role: access.RoleSuperAdmin}, recorder, httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(body)))
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.ErrorNo == 0 || status() != before {
			t.Fatal("credential operations must remain scoped to the current account")
		}
	}
}

func TestClientCredentialPasswordFailuresShareRateLimit(t *testing.T) {
	_, _, engine, user, _ := newPublicSecurityFixture(t)
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	previousDB, previousRuntime := db.Instance, securityRuntime
	previousUserLimiter, previousIPLimiter := reauthUserAttempts, reauthIPAttempts
	db.Instance, securityRuntime.credentials = engine, service
	reauthUserAttempts, reauthIPAttempts = newLoginLimiter(1), newLoginLimiter(10)
	t.Cleanup(func() {
		db.Instance, securityRuntime = previousDB, previousRuntime
		reauthUserAttempts, reauthIPAttempts = previousUserLimiter, previousIPLimiter
	})
	ctx := &appcontext.Context{UserID: user.ID, RecentAuth: true}
	CreateClientCredential(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(`{"client_name":"设备","password":"incorrect"}`)))
	recorder := httptest.NewRecorder()
	RevealProtocolCredentials(ctx, recorder, httptest.NewRequest(http.MethodPost, "/api/security", strings.NewReader(`{"password":"FixtureP12"}`)))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatal("switching from client to legacy protocol entry must not bypass password failure limits")
	}
}
