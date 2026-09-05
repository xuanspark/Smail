package http_server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestConfigRouteAcceptsConfigurationUpdateMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/config/email", nil)

	configRoute(&appcontext.Context{}, recorder, request)

	if recorder.Code == http.StatusMethodNotAllowed {
		t.Fatal("configuration update route rejected the documented PUT method")
	}
}

func TestConfigRouteRejectsUnsupportedMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/config/email", nil)

	configRoute(&appcontext.Context{}, recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported configuration method returned status %d", recorder.Code)
	}
}
