package controllers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"

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

func TestEnsureConfigRequestIDPreservesSafeHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/config/status", nil)
	request.Header.Set("X-Request-ID", "ui-request-42")
	if got := EnsureConfigRequestID(recorder, request); got != "ui-request-42" {
		t.Fatalf("safe request id was not preserved: %q", got)
	}
	if recorder.Header().Get("X-Request-ID") != "ui-request-42" {
		t.Fatalf("response request id header was not preserved: %q", recorder.Header().Get("X-Request-ID"))
	}
}

func TestEnsureConfigRequestIDReplacesUnsafeHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/config/status", nil)
	request.Header.Set("X-Request-ID", "../../secret")
	requestID := EnsureConfigRequestID(recorder, request)
	if requestID == "../../secret" || !requestIDPattern.MatchString(requestID) {
		t.Fatalf("unsafe request id was not replaced: %q", requestID)
	}
}

func TestWriteConfigErrorUsesStableCodeAndHTTPStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "request-99")
	writeConfigError(recorder, config.ErrGenerationConflict)

	if recorder.Code != 409 {
		t.Fatalf("unexpected HTTP status: %d", recorder.Code)
	}
	var payload struct {
		ErrorMsg string                   `json:"errorMsg"`
		Data     response.ConfigErrorData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode configuration error response: %v", err)
	}
	if payload.Data.Code != "CONFIG_GENERATION_CONFLICT" {
		t.Fatalf("unexpected stable error code: %q", payload.Data.Code)
	}
	if payload.Data.RequestID != "request-99" {
		t.Fatalf("unexpected request id: %q", payload.Data.RequestID)
	}
	if payload.ErrorMsg == "" || strings.Contains(payload.ErrorMsg, "generation conflict") {
		t.Fatalf("configuration error message is not a Chinese public message: %q", payload.ErrorMsg)
	}
}
