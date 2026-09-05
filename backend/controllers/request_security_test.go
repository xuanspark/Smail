package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type requestDecoderFixture struct {
	Value string `json:"value"`
}

func TestDecodeJSONRequestRejectsUnsafeBodies(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `{"value":"ok","extra":true}`},
		{name: "trailing value", payload: `{"value":"ok"} {}`},
		{name: "oversized", payload: `{"value":"` + strings.Repeat("a", int(maxJSONRequestBytes)) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.payload))
			if err := decodeJSONRequest(httptest.NewRecorder(), request, &requestDecoderFixture{}); err == nil {
				t.Fatal("unsafe request body was accepted")
			}
		})
	}
}

func TestDecodeJSONRequestAcceptsSingleValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"ok"}`))
	var decoded requestDecoderFixture
	if err := decodeJSONRequest(httptest.NewRecorder(), request, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Value != "ok" {
		t.Fatalf("unexpected value: %q", decoded.Value)
	}
}
