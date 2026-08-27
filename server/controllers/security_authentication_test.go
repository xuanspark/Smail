package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/response"
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
