package credential

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
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

func TestPluginEgressRejectsInvalidRequests(t *testing.T) {
	token := strings.Repeat("a", 64)
	allowed := []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}}
	cases := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"missing_token", func(r *http.Request) { r.Header.Del(egressTokenHeader) }, http.StatusForbidden},
		{"wrong_token", func(r *http.Request) { r.Header.Set(egressTokenHeader, strings.Repeat("b", 64)) }, http.StatusForbidden},
		{"duplicate_token", func(r *http.Request) { r.Header.Add(egressTokenHeader, token) }, http.StatusForbidden},
		{"wrong_host", func(r *http.Request) { r.Host = "other" }, http.StatusBadRequest},
		{"wrong_path", func(r *http.Request) { r.URL.Path = "/other" }, http.StatusBadRequest},
		{"query", func(r *http.Request) { r.URL.RawQuery = "x=1" }, http.StatusBadRequest},
		{"connect", func(r *http.Request) { r.Method = http.MethodConnect }, http.StatusBadRequest},
		{"upgrade", func(r *http.Request) { r.Header.Set("Upgrade", "websocket") }, http.StatusBadRequest},
		{"duplicate_target", func(r *http.Request) { r.Header.Add(egressTargetHeader, "https://allowed.example.com") }, http.StatusForbidden},
		{"http_target", func(r *http.Request) { r.Header.Set(egressTargetHeader, "http://allowed.example.com") }, http.StatusForbidden},
		{"private_target", func(r *http.Request) { r.Header.Set(egressTargetHeader, "https://127.0.0.1") }, http.StatusForbidden},
		{"other_target", func(r *http.Request) { r.Header.Set(egressTargetHeader, "https://other.example.com") }, http.StatusForbidden},
		{"other_port", func(r *http.Request) { r.Header.Set(egressTargetHeader, "https://allowed.example.com:8443") }, http.StatusForbidden},
		{"oversized_body", func(r *http.Request) { r.ContentLength = maxEgressRequestBytes + 1 }, http.StatusRequestEntityTooLarge},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/relay", nil)
			request.Host = "plugin-egress"
			request.Header.Set(egressTokenHeader, token)
			request.Header.Set(egressTargetHeader, "https://allowed.example.com/send")
			item.change(request)
			response := httptest.NewRecorder()
			NewPluginEgressHandler(token, func() []NetworkTarget { return allowed }).ServeHTTP(response, request)
			if response.Code != item.status {
				t.Fatalf("unexpected rejection status: %d", response.Code)
			}
		})
	}
}

func TestPluginEgressEmptyAuthorizationFailsClosed(t *testing.T) {
	for _, authorize := range []func() []NetworkTarget{
		nil,
		func() []NetworkTarget { return nil },
		func() []NetworkTarget { return []NetworkTarget{{Scheme: "invalid", Host: "allowed.example.com"}} },
	} {
		token := strings.Repeat("a", 64)
		request := httptest.NewRequest(http.MethodPost, "/relay", nil)
		request.Host = "plugin-egress"
		request.Header.Set(egressTokenHeader, token)
		request.Header.Set(egressTargetHeader, "https://allowed.example.com/send")
		response := httptest.NewRecorder()
		NewPluginEgressHandler(token, authorize).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("empty authorization was accepted: %d", response.Code)
		}
	}
}

func TestPluginEgressRechecksAuthorizationBeforeOutbound(t *testing.T) {
	token := strings.Repeat("a", 64)
	calls := 0
	handler := NewPluginEgressHandler(token, func() []NetworkTarget {
		calls++
		if calls > 1 {
			return nil
		}
		return []NetworkTarget{{Scheme: "https", Host: "allowed.example.com", Port: 443}}
	})
	request := httptest.NewRequest(http.MethodPost, "/relay", strings.NewReader("payload"))
	request.Host = "plugin-egress"
	request.Header.Set(egressTokenHeader, token)
	request.Header.Set(egressTargetHeader, "https://allowed.example.com/send")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || calls != 2 {
		t.Fatalf("authorization was not rechecked: status=%d calls=%d", response.Code, calls)
	}
}

func TestStripEgressHeaders(t *testing.T) {
	header := make(http.Header)
	for _, name := range []string{egressTokenHeader, egressTargetHeader, "Proxy-Authorization", "X-Hop", "Upgrade"} {
		header.Set(name, "sensitive")
	}
	header.Set("Connection", "X-Hop, Upgrade")
	header.Set("Content-Type", "application/json")
	stripEgressHeaders(header)
	if len(header) != 1 || header.Get("Content-Type") != "application/json" {
		t.Fatal("internal or hop-by-hop headers survived filtering")
	}
}

type egressTrackedBody struct {
	io.Reader
	closed bool
}

func (b *egressTrackedBody) Close() error {
	b.closed = true
	return nil
}

func TestRestrictedTransportsCloseRejectedRequestBody(t *testing.T) {
	for _, transport := range []http.RoundTripper{restrictedRoundTripper{}, &pluginEgressTransport{}} {
		body := &egressTrackedBody{Reader: strings.NewReader("payload")}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/", body)
		if _, err := transport.RoundTrip(request); err == nil || !body.closed {
			t.Fatal("rejected request body was not closed")
		}
	}
}

type egressIdleTransport struct {
	http.RoundTripper
	closed bool
}

func (t *egressIdleTransport) CloseIdleConnections() { t.closed = true }

func TestRestrictedTransportForwardsIdleConnectionCleanup(t *testing.T) {
	transport := &egressIdleTransport{}
	client := &http.Client{Transport: restrictedRoundTripper{transport: &pluginEgressTransport{transport: transport}}}
	client.CloseIdleConnections()
	if !transport.closed {
		t.Fatal("idle connection cleanup was not forwarded")
	}
}
