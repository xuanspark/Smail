package framework

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestAuthenticatedHandlerRequiresStartupToken(t *testing.T) {
	plugin := &Plugin{authToken: "startup-token"}
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := plugin.authenticatedHandler(next)

	for _, test := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong", token: "wrong-token", status: http.StatusUnauthorized},
		{name: "valid", token: "startup-token", status: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/Health", nil)
			if test.token != "" {
				request.Header.Set("X-Smail-Plugin-Token", test.token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestAuthenticatedHandlerRejectsEmptyConfiguredToken(t *testing.T) {
	plugin := &Plugin{}
	handler := plugin.authenticatedHandler(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		t.Fatal("unauthenticated request reached handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "/Health", nil)
	request.Header.Set("X-Smail-Plugin-Token", "unexpected")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDecodePluginRequestRejectsOversizedPayload(t *testing.T) {
	body := bytes.NewBufferString(`"` + strings.Repeat("x", int(maxPluginRPCBytes)) + `"`)
	request := httptest.NewRequest(http.MethodPost, "/", body)
	recorder := httptest.NewRecorder()
	var payload json.RawMessage
	if decodePluginRequest(recorder, request, &payload) {
		t.Fatal("oversized payload was accepted")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleConfigDoesNotExposeActionError(t *testing.T) {
	plugin := &Plugin{}
	handler := plugin.handleConfig(func(json.RawMessage) error {
		return errors.New("secret-token")
	})
	request := httptest.NewRequest(http.MethodPost, "/Configure", bytes.NewBufferString(`{}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if strings.Contains(recorder.Body.String(), "secret-token") {
		t.Fatal("configuration error exposed secret material")
	}
}

func TestLimitedHandlerCapsConcurrentRequests(t *testing.T) {
	plugin := &Plugin{}
	started := make(chan struct{}, pluginRPCConcurrencyLimit)
	release := make(chan struct{})
	var active int
	var peak int
	var mu sync.Mutex
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	handler := plugin.limitedHandler(next)
	var wg sync.WaitGroup
	for i := 0; i < pluginRPCConcurrencyLimit+8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodPost, "/Health", nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
		}()
	}
	for i := 0; i < pluginRPCConcurrencyLimit; i++ {
		<-started
	}
	mu.Lock()
	if peak != pluginRPCConcurrencyLimit {
		mu.Unlock()
		close(release)
		wg.Wait()
		t.Fatalf("peak concurrency = %d, want %d", peak, pluginRPCConcurrencyLimit)
	}
	mu.Unlock()
	close(release)
	wg.Wait()
}
