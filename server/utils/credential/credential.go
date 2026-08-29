package credential

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultTimeoutMS = 3000
	minTimeoutMS     = 1000
	maxTimeoutMS     = 30000
)

func ValidateSecret(value string, maxLength int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLength || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("invalid credential")
	}
	return value, nil
}

func NormalizeHTTPSURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.Contains(raw, "#") {
		return "", errors.New("invalid endpoint")
	}

	endpoint, err := url.ParseRequestURI(raw)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return "", errors.New("invalid endpoint")
	}
	if port := endpoint.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("invalid endpoint")
		}
	}
	host := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", errors.New("invalid endpoint")
	}
	if address, err := netip.ParseAddr(host); err == nil && !IsPublicRemoteIP(address) {
		return "", errors.New("invalid endpoint")
	}
	return endpoint.String(), nil
}

func IsPublicRemoteIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if address.Is6() && !globalIPv6UnicastPrefix.Contains(address) {
		return false
	}
	for _, prefix := range restrictedRemotePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var globalIPv6UnicastPrefix = netip.MustParsePrefix("2000::/3")

var restrictedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func NewRestrictedHTTPClient(timeoutMS int) *http.Client {
	return NewRestrictedHTTPClientForTargets(timeoutMS, nil)
}

// NetworkTarget 描述插件允许访问的网络目标。
type NetworkTarget struct {
	Scheme string
	Host   string
	Port   int
}

// NewRestrictedHTTPClientForTargets 创建仅允许访问授权目标的 HTTP 客户端。
func NewRestrictedHTTPClientForTargets(timeoutMS int, targets []NetworkTarget) *http.Client {
	timeout := time.Duration(NormalizeTimeoutMS(timeoutMS)) * time.Millisecond
	allowedTargets := normalizeNetworkTargets(targets)
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return restrictedDialContext(ctx, network, address, allowedTargets)
		},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: restrictedRoundTripper{transport: transport, targets: allowedTargets},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return validateRestrictedRequest(request, allowedTargets, "redirect target is not authorized")
		},
	}
}

type restrictedRoundTripper struct {
	transport http.RoundTripper
	targets   []NetworkTarget
}

func (r restrictedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := validateRestrictedRequest(request, r.targets, "remote target is not authorized"); err != nil {
		return nil, err
	}
	return r.transport.RoundTrip(request)
}

func validateRestrictedRequest(request *http.Request, targets []NetworkTarget, targetError string) error {
	if request == nil || request.URL == nil {
		return errors.New("invalid endpoint")
	}
	if _, err := NormalizeHTTPSURL(request.URL.String()); err != nil {
		return err
	}
	if !matchesNetworkTarget(request.URL, targets) {
		return errors.New(targetError)
	}
	return nil
}

func normalizeNetworkTargets(targets []NetworkTarget) []NetworkTarget {
	result := make([]NetworkTarget, 0, len(targets))
	for _, target := range targets {
		scheme := strings.ToLower(strings.TrimSpace(target.Scheme))
		host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(target.Host)), ".")
		port := target.Port
		if port == 0 {
			if scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}
		if (scheme != "https" && scheme != "http") || host == "" || port < 1 || port > 65535 {
			continue
		}
		result = append(result, NetworkTarget{Scheme: scheme, Host: host, Port: port})
	}
	return result
}

func matchesNetworkTarget(endpoint *url.URL, targets []NetworkTarget) bool {
	if len(targets) == 0 {
		return true
	}
	port := 443
	if strings.EqualFold(endpoint.Scheme, "http") {
		port = 80
	}
	if endpoint.Port() != "" {
		port, _ = strconv.Atoi(endpoint.Port())
	}
	host := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	scheme := strings.ToLower(endpoint.Scheme)
	for _, target := range targets {
		if target.Scheme == scheme && target.Host == host && target.Port == port {
			return true
		}
	}
	return false
}

func restrictedDialContext(ctx context.Context, network, address string, targets []NetworkTarget) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid remote address")
	}
	if _, err := strconv.Atoi(port); err != nil || (len(targets) > 0 && !matchesNetworkTarget(&url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}, targets) && !matchesNetworkTarget(&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}, targets)) {
		return nil, errors.New("remote target is not authorized")
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(resolved) == 0 {
		return nil, errors.New("remote address resolution failed")
	}
	approved := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		candidate, ok := netip.AddrFromSlice(item.IP)
		if !ok || !IsPublicRemoteIP(candidate) {
			return nil, errors.New("remote address is not public")
		}
		candidate = candidate.Unmap()
		if network == "tcp4" && !candidate.Is4() || network == "tcp6" && !candidate.Is6() {
			continue
		}
		approved = append(approved, candidate)
	}
	if len(approved) == 0 {
		return nil, errors.New("remote address is unavailable")
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, candidate := range approved {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connect to remote endpoint: %w", lastErr)
}

func NormalizeTimeoutMS(value int) int {
	if value <= 0 {
		return defaultTimeoutMS
	}
	if value < minTimeoutMS {
		return minTimeoutMS
	}
	if value > maxTimeoutMS {
		return maxTimeoutMS
	}
	return value
}
