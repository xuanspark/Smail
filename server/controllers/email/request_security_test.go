package email

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type emailRequestDecoderFixture struct {
	Value string `json:"value"`
}

func TestDecodeEmailJSONRequestRejectsUnsafeBodies(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `{"value":"ok","extra":true}`},
		{name: "trailing value", payload: `{"value":"ok"} {}`},
		{name: "oversized", payload: `{"value":"` + strings.Repeat("a", int(maxEmailJSONRequestBytes)) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.payload))
			if err := decodeEmailJSONRequest(httptest.NewRecorder(), request, &emailRequestDecoderFixture{}, maxEmailJSONRequestBytes); err == nil {
				t.Fatal("unsafe request body was accepted")
			}
		})
	}
}

func TestDecodeEmailJSONRequestAcceptsSingleValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"ok"}`))
	var decoded emailRequestDecoderFixture
	if err := decodeEmailJSONRequest(httptest.NewRecorder(), request, &decoded, maxEmailJSONRequestBytes); err != nil {
		t.Fatal(err)
	}
	if decoded.Value != "ok" {
		t.Fatalf("unexpected value: %q", decoded.Value)
	}
}
