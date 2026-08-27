package framework

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodePluginRequestRejectsInvalidMethodAndOversizedBody(t *testing.T) {
	var dto HookDTO
	methodRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	methodRecorder := httptest.NewRecorder()
	if decodePluginRequest(methodRecorder, methodRequest, &dto) {
		t.Fatal("GET request was accepted")
	}
	if methodRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", methodRecorder.Code, http.StatusMethodNotAllowed)
	}

	oversizedBody := append([]byte(`{"ServerVersion":"`), bytes.Repeat([]byte{'a'}, int(maxPluginRPCBytes))...)
	oversizedBody = append(oversizedBody, []byte(`"}`)...)
	oversizedRequest := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversizedBody))
	oversizedRecorder := httptest.NewRecorder()
	if decodePluginRequest(oversizedRecorder, oversizedRequest, &dto) {
		t.Fatal("oversized request was accepted")
	}
	if oversizedRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", oversizedRecorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWritePluginResponseRejectsOversizedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := writePluginResponse(recorder, "text/plain", bytes.Repeat([]byte{'a'}, int(maxPluginRPCBytes)+1)); err == nil {
		t.Fatal("oversized response was accepted")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}
