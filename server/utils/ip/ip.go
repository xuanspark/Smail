package ip

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	publicIPv4Endpoint = "https://api.ipify.org"
	publicIPv6Endpoint = "https://api6.ipify.org"
	maxIPResponseBytes = 64
)

var (
	publicIPClient = &http.Client{Timeout: 3 * time.Second}
	cacheMu        sync.Mutex
	cachedIPv4     string
	cachedIPv6     string
)

func GetIp() string {
	value := GetIPv4()
	if value == "" {
		return "[Your Server IP]"
	}
	return value
}

func GetIPv4() string {
	return getPublicIP(publicIPv4Endpoint, false)
}

func GetIPv6() string {
	return getPublicIP(publicIPv6Endpoint, true)
}

func getPublicIP(endpoint string, wantIPv6 bool) string {
	cacheMu.Lock()
	if wantIPv6 && cachedIPv6 != "" {
		value := cachedIPv6
		cacheMu.Unlock()
		return value
	}
	if !wantIPv6 && cachedIPv4 != "" {
		value := cachedIPv4
		cacheMu.Unlock()
		return value
	}
	cacheMu.Unlock()

	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	value, err := fetchPublicIPForFamily(publicIPClient, request, wantIPv6)
	if err != nil {
		return ""
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if wantIPv6 {
		if cachedIPv6 == "" {
			cachedIPv6 = value
		}
		return cachedIPv6
	}
	if cachedIPv4 == "" {
		cachedIPv4 = value
	}
	return cachedIPv4
}

func fetchPublicIPForFamily(client *http.Client, request *http.Request, wantIPv6 bool) (string, error) {
	value, err := fetchPublicIP(client, request)
	if err != nil {
		return "", err
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.Is6() != wantIPv6 {
		return "", errors.New("public IP address family does not match")
	}
	return address.String(), nil
}

func fetchPublicIP(client *http.Client, request *http.Request) (string, error) {
	if client == nil || request == nil || request.URL == nil || request.URL.Scheme != "https" {
		return "", errors.New("invalid public IP request")
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", errors.New("public IP service returned an unexpected status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxIPResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxIPResponseBytes {
		return "", errors.New("public IP response is too large")
	}
	value := strings.TrimSpace(string(body))
	parsed, err := netip.ParseAddr(value)
	if err != nil || !isPublicIP(parsed) {
		return "", errors.New("public IP response is invalid")
	}
	return parsed.String(), nil
}

func isPublicIP(value netip.Addr) bool {
	return value.IsValid() && value.IsGlobalUnicast() &&
		!value.IsPrivate() && !value.IsLoopback() &&
		!value.IsLinkLocalUnicast() && !value.IsLinkLocalMulticast() &&
		!value.IsMulticast() && !value.IsUnspecified() &&
		!net.IP(value.AsSlice()).IsInterfaceLocalMulticast()
}
