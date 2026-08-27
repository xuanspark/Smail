package framework

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
)

type controlledResponseWriter struct {
	header   http.Header
	status   int
	writeN   int
	writeErr error
}

func (w *controlledResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *controlledResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *controlledResponseWriter) Write(body []byte) (int, error) {
	if w.writeN < 0 {
		return len(body), w.writeErr
	}
	return w.writeN, w.writeErr
}

func TestDecodePluginRequestRejectsTrailingJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{} {}`))
	recorder := httptest.NewRecorder()
	var dto HookDTO
	if decodePluginRequest(recorder, request, &dto) {
		t.Fatal("trailing JSON was accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestWritePluginResponsePropagatesWriteFailure(t *testing.T) {
	writeErr := errors.New("write failed")
	writer := &controlledResponseWriter{writeN: 0, writeErr: writeErr}
	if err := writePluginResponse(writer, "application/json", []byte("{}")); !errors.Is(err, writeErr) {
		t.Fatalf("write error was not propagated: %v", err)
	}
}

func TestWritePluginResponseRejectsShortWrite(t *testing.T) {
	writer := &controlledResponseWriter{writeN: 1}
	if err := writePluginResponse(writer, "application/json", []byte("{}")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write was not propagated: %v", err)
	}
}

func TestHookDTOIncludesEmailAuthentication(t *testing.T) {
	dto := HookDTO{
		Email: &parsemail.Email{
			Subject:        "authentication result",
			Authentication: parsemail.NewEmailAuthentication(false, false),
		},
	}
	body, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HookDTO
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Email == nil || decoded.Email.Authentication == nil || !decoded.Email.Authentication.Dangerous {
		t.Fatalf("authentication result was not preserved: %+v", decoded.Email)
	}
}

func TestOlderPluginShapeIgnoresNewAuthenticationField(t *testing.T) {
	dto := HookDTO{
		Email: &parsemail.Email{
			Subject:        "compatibility",
			Authentication: parsemail.NewEmailAuthentication(true, false),
		},
	}
	body, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var oldDTO struct {
		Email *struct {
			Subject string
		}
	}
	if err := json.Unmarshal(body, &oldDTO); err != nil {
		t.Fatal(err)
	}
	if oldDTO.Email == nil || oldDTO.Email.Subject != "compatibility" {
		t.Fatalf("older plugin shape could not read the message: %+v", oldDTO.Email)
	}
}
