package controllers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestSetupReturnsJSONForUnsupportedOperation(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/setup", strings.NewReader(`{"step":"unknown","action":"unknown"}`))
	response := httptest.NewRecorder()

	Setup(&appcontext.Context{}, response, request)

	if response.Code != 200 {
		t.Fatalf("setup status = %d, want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("setup content type = %q, want JSON", contentType)
	}
	var payload struct {
		ErrorNo  int    `json:"errorNo"`
		ErrorMsg string `json:"errorMsg"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if payload.ErrorNo != 100 || payload.ErrorMsg != "不支持的初始化操作" {
		t.Fatalf("unexpected setup response: %+v", payload)
	}
}
