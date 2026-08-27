package controllers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
)

func TestWriteConfigErrorKeepsStableAuthorizationCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeConfigError(recorder, &access.Denial{Code: access.CodeRecoveryCodeRequired})

	var payload struct {
		ErrorNo int            `json:"errorNo"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode authorization error response: %v", err)
	}
	if payload.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("unexpected authorization error number: %d", payload.ErrorNo)
	}
	if payload.Data["code"] != string(access.CodeRecoveryCodeRequired) {
		t.Fatalf("stable authorization code was not exposed: %#v", payload.Data)
	}
}

func TestWriteConfigErrorDoesNotExposeInternalErrorText(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeConfigError(recorder, &access.Denial{Code: access.CodeRecentAuthRequired})

	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"password", "secret", "token", "nonce", "ticket", "challenge"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("authorization response contains sensitive response text: %s", body)
		}
	}
}
