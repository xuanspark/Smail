package http_server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func resetEntryFixture() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Smail Reset</title>")}}
}

func TestResetEntryIsNeverServedByMainWebServer(t *testing.T) {
	handler := resetEntryHandler(resetEntryFixture())
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, "/Reset", nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("method %s returned status %d", method, recorder.Code)
		}
	}
}

func TestRouterRejectsAllResetPathVariantsBeforeSPAFallback(t *testing.T) {
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/reset",
		"/reset/",
		"/reset/anything",
		"/Reset",
		"/Reset/",
		"/RESET",
		"/RESET/anything",
		"/r%65set",
		"/r%65set/anything",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("reset path returned status %d", recorder.Code)
			}
			if body := recorder.Body.String(); body == string(resetEntryFixture().(fstest.MapFS)["index.html"].Data) {
				t.Fatal("reset path returned SPA index")
			}
		})
	}
}

func TestIsReservedResetPath(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "/reset", want: true},
		{path: "/reset/", want: true},
		{path: "/RESET/anything", want: true},
		{path: "/forgot-password", want: false},
		{path: "/mail/reset", want: false},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := isReservedResetPath(test.path); got != test.want {
				t.Fatalf("isReservedResetPath(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}
