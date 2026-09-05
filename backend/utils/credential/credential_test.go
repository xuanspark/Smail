package credential

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestValidateSecret(t *testing.T) {
	value, err := ValidateSecret(" token-value ", 64)
	if err != nil || value != "token-value" {
		t.Fatal("valid credential was rejected")
	}
	for _, input := range []string{"", "line\nbreak"} {
		if _, err := ValidateSecret(input, 64); err == nil {
			t.Fatal("invalid credential was accepted")
		}
	}
}

func TestNormalizeHTTPSURL(t *testing.T) {
	value, err := NormalizeHTTPSURL("https://model.example.com/v1/predict?model=mail")
	if err != nil || value != "https://model.example.com/v1/predict?model=mail" {
		t.Fatal("valid HTTPS endpoint was rejected")
	}
	for _, input := range []string{
		"http://model.example.com/v1/predict",
		"https://user:password@model.example.com/v1/predict",
		"https://model.example.com/v1/predict#fragment",
		"https:///v1/predict",
		"https://127.0.0.1/v1/predict",
		"https://10.0.0.1/v1/predict",
		"https://169.254.169.254/latest/meta-data",
		"https://100.64.0.1/v1/predict",
		"https://[::1]/v1/predict",
		"https://localhost/v1/predict",
		"https://service.localhost/v1/predict",
	} {
		if _, err := NormalizeHTTPSURL(input); err == nil {
			t.Fatalf("unsafe endpoint was accepted: %s", input)
		}
	}
}

func TestIsPublicRemoteIPRejectsReservedAddresses(t *testing.T) {
	for _, value := range []string{
		"0.0.0.0",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.0.1",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"240.0.0.1",
		"::1",
		"64:ff9b::7f00:1",
		"100::1",
		"2001::1",
		"2001:db8::1",
		"2002:7f00:1::",
		"3fff::1",
		"4000::1",
		"fc00::1",
		"fe80::1",
	} {
		if IsPublicRemoteIP(netip.MustParseAddr(value)) {
			t.Fatalf("reserved address was accepted: %s", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !IsPublicRemoteIP(netip.MustParseAddr(value)) {
			t.Fatalf("public address was rejected: %s", value)
		}
	}
}

func TestRestrictedHTTPClientRejectsLocalEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewRestrictedHTTPClient(1000)
	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("restricted client connected to a local endpoint")
	}
}

func TestRestrictedHTTPClientRejectsPrivateRedirect(t *testing.T) {
	client := NewRestrictedHTTPClient(1000)
	request := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/internal", nil)
	if err := client.CheckRedirect(request, nil); err == nil {
		t.Fatal("restricted client accepted a private redirect")
	}
}

func TestRestrictedHTTPClientRejectsCrossTargetRedirect(t *testing.T) {
	client := NewRestrictedHTTPClientForTargets(1000, []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}})
	request := httptest.NewRequest(http.MethodGet, "https://other.example.com/internal", nil)
	if err := client.CheckRedirect(request, nil); err == nil {
		t.Fatal("restricted client accepted a redirect outside the authorized target")
	}
}

func TestRestrictedHTTPClientRejectsInitialHTTPRequest(t *testing.T) {
	client := NewRestrictedHTTPClientForTargets(1000, []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}})
	if _, err := client.Get("http://allowed.example.com:443/internal"); err == nil {
		t.Fatal("restricted client accepted an initial HTTP request")
	}
}

func TestRestrictedHTTPClientRejectsInitialUnauthorizedPort(t *testing.T) {
	client := NewRestrictedHTTPClientForTargets(1000, []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}})
	if _, err := client.Get("https://allowed.example.com:8443/internal"); err == nil {
		t.Fatal("restricted client accepted an unauthorized initial port")
	}
}
func TestRestrictedHTTPClientAllowsAuthorizedRedirect(t *testing.T) {
	client := NewRestrictedHTTPClientForTargets(1000, []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}})
	request := httptest.NewRequest(http.MethodGet, "https://allowed.example.com/next", nil)
	if err := client.CheckRedirect(request, nil); err != nil {
		t.Fatalf("authorized redirect was rejected: %v", err)
	}
}

func TestNormalizeTimeoutMS(t *testing.T) {
	if NormalizeTimeoutMS(0) != defaultTimeoutMS || NormalizeTimeoutMS(1) != minTimeoutMS || NormalizeTimeoutMS(60000) != maxTimeoutMS {
		t.Fatal("timeout limit was not normalized")
	}
}
