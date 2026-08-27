package ip

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const (
	publicIPEndpoint   = "https://api.ipify.org"
	maxIPResponseBytes = 64
)

var (
	ip             string
	publicIPClient = &http.Client{Timeout: 3 * time.Second}
)

func GetIp() string {
	if ip != "" {
		return ip
	}

	request, err := http.NewRequest(http.MethodGet, publicIPEndpoint, nil)
	if err != nil {
		return "[Your Server IP]"
	}
	publicIP, err := fetchPublicIP(publicIPClient, request)
	if err != nil {
		return "[Your Server IP]"
	}
	ip = publicIP
	return publicIP
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
