package validation

import (
	"errors"
	"strings"
)

var ErrNotificationRecipient = errors.New("notification address does not accept mail")

// notification 为系统专属发信账号，禁止普通用户占用，且拒收任何外部来信。
const (
	NotificationLocalPart = "notification"
)

func NotificationAddress(domain string) string {
	if domain == "" {
		return ""
	}
	return NotificationLocalPart + "@" + domain
}

func IsNotificationAddress(address string, domains []string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(address)), "@")
	if len(parts) != 2 {
		return false
	}
	if parts[0] != NotificationLocalPart {
		return false
	}
	for _, domain := range domains {
		if strings.EqualFold(parts[1], domain) {
			return true
		}
	}
	return false
}

func IsAuthorizedNotificationSender(address, domain string) bool {
	if domain == "" {
		return false
	}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(address)), "@")
	if len(parts) != 2 || !strings.EqualFold(parts[1], domain) {
		return false
	}
	return parts[0] == NotificationLocalPart
}
